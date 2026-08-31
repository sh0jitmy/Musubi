// Copyright 2026 [Copyright Holder]
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// Author: [YOUR_NAME]

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	_ "github.com/glebarez/go-sqlite"
	"github.com/sh0jitmy/musubi/ent/target"
	"github.com/sh0jitmy/musubi/internal/collector"
	"github.com/sh0jitmy/musubi/internal/common/notification"
	"github.com/sh0jitmy/musubi/internal/common/types"
	"github.com/sh0jitmy/musubi/internal/database"
	"github.com/sh0jitmy/musubi/internal/gateway"
	"github.com/sh0jitmy/musubi/internal/state"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dbDriver := os.Getenv("DATABASE_DRIVER")
	if dbDriver == "" {
		dbDriver = "sqlite3"
	}
	dbDSN := os.Getenv("DATABASE_DSN")
	if dbDSN == "" {
		dbDSN = "musubi.db?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)"
	}

	slog.Info("Initializing database connection", "driver", dbDriver)
	client, err := database.NewClient(ctx, dbDriver, dbDSN)
	if err != nil {
		slog.Error("Failed to initialize database", "error", err)
		os.Exit(1)
	}
	defer func() {
		_ = client.Close()
	}()

	if seedErr := database.SeedAdminUser(ctx, client); seedErr != nil {
		slog.Warn("Failed to seed admin user", "error", seedErr)
	}

	// Start in-process Log Retention Cleaner Worker (no OS cron needed)
	retentionHours := 24
	if rh := os.Getenv("RETENTION_INTERVAL_HOURS"); rh != "" {
		if val, convErr := strconv.Atoi(rh); convErr == nil && val > 0 {
			retentionHours = val
		}
	}
	retentionDays := 30
	if rd := os.Getenv("RETENTION_DAYS"); rd != "" {
		if val, convErr := strconv.Atoi(rd); convErr == nil && val > 0 {
			retentionDays = val
		}
	}
	database.StartBackgroundCleaner(ctx, client, time.Duration(retentionHours)*time.Hour, retentionDays)

	hub := notification.NewHub(1000)

	// State repository with transition handler for DB recording and SSE publish
	stateRepo := state.NewRepository(func(t types.StateTransition) {
		hub.Publish("state.transition", t)
		go func() {
			dbCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, _ = client.StateTransitionLog.Create().
				SetID(fmt.Sprintf("st-%d", time.Now().UnixNano())).
				SetTarget(t.Target).
				SetStateKey(t.StateKey).
				SetOldValue(t.OldValue).
				SetNewValue(t.NewValue).
				SetTrigger(t.Trigger).
				Save(dbCtx)
		}()
	})

	// Start SNMP Trap / Inform listener
	trapPort := os.Getenv("SNMP_TRAP_PORT")
	if trapPort == "" {
		trapPort = "162"
	}
	trapAddr := fmt.Sprintf(":%s", trapPort)
	trapListener := collector.NewListener(trapAddr, func(targetHost string, oid string, val any, trigger string) {
		// Lookup target by host name/IP if exists, else use host directly
		targetName := targetHost
		tCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		if tg, qErr := client.Target.Query().Where(target.HostEQ(targetHost)).First(tCtx); qErr == nil && tg != nil {
			targetName = tg.Name
		}
		cancel()

		stateRepo.SetRaw(targetName, oid, val, trigger)
	})

	if lErr := trapListener.Start(); lErr != nil {
		slog.Warn("Could not start SNMP Trap listener on port, proceeding...", "port", trapPort, "error", lErr)
	} else {
		slog.Info("Started SNMP Trap / Inform listener", "port", trapPort)
		defer trapListener.Stop()
	}

	server, err := gateway.NewServer(client, hub, stateRepo)
	if err != nil {
		slog.Error("Failed to initialize gateway server", "error", err)
		os.Exit(1)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	slog.Info("Starting Musubi server", "port", port)
	go func() {
		if err := server.Engine.Run(fmt.Sprintf(":%s", port)); err != nil {
			slog.Error("Server error", "error", err)
		}
	}()

	<-ctx.Done()
	slog.Info("Shutting down Musubi server...")
	time.Sleep(500 * time.Millisecond)
}
