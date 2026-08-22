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
	"syscall"
	"time"

	_ "github.com/glebarez/go-sqlite"
	"github.com/sh0jitmy/musubi/internal/common/notification"
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

	hub := notification.NewHub(1000)
	stateRepo := state.NewRepository(nil)

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
