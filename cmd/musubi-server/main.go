// Copyright 2026 Musubi Contributors
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
// Author: sh0jitmy

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/glebarez/go-sqlite"
	"github.com/sh0jitmy/musubi/ent/target"
	"github.com/sh0jitmy/musubi/internal/collector"
	"github.com/sh0jitmy/musubi/internal/common/config"
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

	cfg, err := config.Load("")
	if err != nil {
		slog.Warn("Could not load config file, proceeding with defaults and environment variables", "error", err)
		cfg = config.DefaultConfig()
	}

	slog.Info("Initializing database connection", "driver", cfg.Database.Driver)
	client, err := database.NewClient(ctx, cfg.Database.Driver, cfg.Database.DSN)
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
	database.StartBackgroundCleaner(ctx, client, time.Duration(cfg.Retention.IntervalHours)*time.Hour, cfg.Retention.Days)

	// Start in-process Scheduled Backup Worker
	if cfg.Backup.Enabled {
		database.StartBackgroundBackup(
			ctx,
			client,
			time.Duration(cfg.Backup.IntervalHours)*time.Hour,
			cfg.Backup.Directory,
			cfg.Backup.RetentionCount,
		)
	}

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
	trapPort := cfg.Server.TrapPort
	if tp := os.Getenv("SNMP_TRAP_PORT"); tp != "" {
		trapPort = tp
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
	server.BackupDir = cfg.Backup.Directory

	port := cfg.Server.Port
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}

	slog.Info("Starting Musubi server", "port", port, "backup_dir", cfg.Backup.Directory)
	go func() {
		if err := server.Engine.Run(fmt.Sprintf(":%s", port)); err != nil {
			slog.Error("Server error", "error", err)
		}
	}()

	<-ctx.Done()
	slog.Info("Shutting down Musubi server...")
	time.Sleep(500 * time.Millisecond)
}
