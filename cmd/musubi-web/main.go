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
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sh0jitmy/musubi/internal/web"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	portFlag := flag.String("port", "3001", "Port to serve the Musubi Web Frontend")
	apiEndpointFlag := flag.String("api-endpoint", "http://localhost:8080", "Upstream Musubi Core Server API URL")
	ssgExportDir := flag.String("ssg-export", "", "Directory to export pre-rendered static site and exit (SSG mode)")
	flag.Parse()

	port := *portFlag
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}
	if p := os.Getenv("WEB_PORT"); p != "" {
		port = p
	}

	apiEndpoint := *apiEndpointFlag
	if ep := os.Getenv("MUSUBI_API_URL"); ep != "" {
		apiEndpoint = ep
	}

	client := web.NewClient(apiEndpoint, 5*time.Second)

	// If SSG export is requested, generate static site and exit
	if *ssgExportDir != "" {
		slog.Info("Executing SSG (Static Site Generation) export...", "dir", *ssgExportDir)
		if err := web.ExportStaticSite(*ssgExportDir, client); err != nil {
			slog.Error("SSG export failed", "error", err)
			os.Exit(1)
		}
		slog.Info("SSG export completed successfully!", "dir", *ssgExportDir)
		return
	}

	server, err := web.NewServer(client, port)
	if err != nil {
		slog.Error("Failed to initialize web server", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%s", port),
		Handler:           server.Engine,
		ReadHeaderTimeout: 10 * time.Second,
	}

	slog.Info("Starting Musubi Web Frontend Server (Docker-Free HTMX)", "port", port, "upstream_api", apiEndpoint)
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Web server listen error", "error", err)
		}
	}()

	<-ctx.Done()
	slog.Info("Shutting down Musubi Web Frontend Server...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		slog.Error("Web server forced to shutdown", "error", err)
	}
	slog.Info("Musubi Web Frontend Server stopped cleanly")
}
