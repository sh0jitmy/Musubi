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

package web

import (
	"context"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
)

// ExportStaticSite compiles and renders all pages and static assets to an output directory
func ExportStaticSite(outputDir string, client *Client) error {
	if outputDir == "" {
		outputDir = "dist"
	}

	outputDir = filepath.Clean(outputDir)
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	// 1. Copy static assets
	staticDir := filepath.Join(outputDir, "static")
	if err := os.MkdirAll(staticDir, 0o750); err != nil {
		return fmt.Errorf("create static dir: %w", err)
	}

	walkErr := fs.WalkDir(EmbeddedAssets, "static", func(path string, d fs.DirEntry, itemErr error) error {
		if itemErr != nil {
			return itemErr
		}
		rel, err := filepath.Rel("static", path)
		if err != nil {
			return err
		}
		targetPath := filepath.Clean(filepath.Join(staticDir, rel))

		if d.IsDir() {
			return os.MkdirAll(targetPath, 0o750)
		}

		srcFile, err := EmbeddedAssets.Open(path)
		if err != nil {
			return err
		}
		defer func() {
			_ = srcFile.Close()
		}()

		//nolint:gosec // trusted local SSG export path
		dstFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		defer func() {
			_ = dstFile.Close()
		}()

		_, err = io.Copy(dstFile, srcFile)
		return err
	})
	if walkErr != nil {
		return fmt.Errorf("copy static assets: %w", walkErr)
	}

	// 2. Parse templates
	tmpl, err := template.ParseFS(EmbeddedAssets, "templates/*.html", "templates/components/*.html")
	if err != nil {
		return fmt.Errorf("parse templates for ssg: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var (
		metrics     *SystemMetrics
		targets     []TargetViewModel
		transitions []StateTransitionViewModel
		jobs        []JobViewModel
		auditLogs   []AuditLogViewModel
		scenarios   []ScenarioViewModel
	)

	if client != nil {
		metrics, _ = client.GetSystemMetrics(ctx)
		targets, _ = client.ListTargets(ctx)
		transitions, _ = client.ListStateTransitions(ctx)
		jobs, _ = client.ListJobs(ctx)
		auditLogs, _ = client.ListAuditLogs(ctx)
		scenarios, _ = client.ListScenarios(ctx)
	}

	if metrics == nil {
		metrics = &SystemMetrics{CPUPercent: 1.5, MemHeapMB: 12.0, MemSysMB: 26.5, MemRSSMB: 19.1, Goroutines: 12, BandwidthKBps: 4.8}
	}
	if len(targets) == 0 {
		targets = []TargetViewModel{
			{Name: "spine1", Host: "127.0.0.1", Port: 161, Status: "ONLINE", CredentialID: "v2c-default", UpdatedAtFormatted: time.Now().Format("2006-01-02 15:04:05")},
		}
	}
	if len(transitions) == 0 {
		transitions = []StateTransitionViewModel{
			{Target: "spine1", StateKey: ".1.3.6.1.2.1.1.1.0", OldValue: "", NewValue: "Musubi Mock SNMP Linux 6.x", Trigger: "BULKGET", CreatedAtFormatted: time.Now().Format("2006-01-02 15:04:05")},
		}
	}
	if len(jobs) == 0 {
		jobs = []JobViewModel{
			{ID: "job-demo-001", ScenarioID: "spine1-snmp-check", ScenarioVersion: 1, Status: "SUCCESS", TriggeredBy: "ssg-build", CreatedAtFormatted: time.Now().Format("2006-01-02 15:04:05")},
		}
	}
	if len(auditLogs) == 0 {
		auditLogs = []AuditLogViewModel{
			{ID: "audit-001", UserID: "admin", Role: "Administrator", Action: "SYSTEM_INIT", TargetID: "spine1", IP: "127.0.0.1", CreatedAtFormatted: time.Now().Format("2006-01-02 15:04:05")},
		}
	}
	if len(scenarios) == 0 {
		scenarios = []ScenarioViewModel{
			{Name: "snmp-get-sysdescr", Description: "System Descr取得", TargetLocks: []string{"spine1"}, Version: 1, CreatedAtFormatted: time.Now().Format("2006-01-02 15:04:05")},
		}
	}

	// 3. Render index.html (Dashboard Overview)
	indexPath := filepath.Clean(filepath.Join(outputDir, "index.html"))
	//nolint:gosec // trusted local SSG export path
	indexFile, err := os.OpenFile(indexPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open index.html: %w", err)
	}
	defer func() {
		_ = indexFile.Close()
	}()

	dashboardData := gin.H{
		"Title":       "統合ダッシュボード (SSG)",
		"ActiveNav":   "overview",
		"Metrics":     metrics,
		"TargetsData": gin.H{"Targets": targets},
		"MibsData":    gin.H{"Transitions": transitions},
		"JobsData":    gin.H{"Jobs": jobs},
		"AuditData":   gin.H{"AuditLogs": auditLogs},
	}
	if renderErr := tmpl.ExecuteTemplate(indexFile, "layout", dashboardData); renderErr != nil {
		return fmt.Errorf("render index.html: %w", renderErr)
	}

	// 4. Render scenarios.html (Scenario Studio)
	scenariosPath := filepath.Clean(filepath.Join(outputDir, "scenarios.html"))
	//nolint:gosec // trusted local SSG export path
	scenariosFile, err := os.OpenFile(scenariosPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open scenarios.html: %w", err)
	}
	defer func() {
		_ = scenariosFile.Close()
	}()

	scenariosData := gin.H{
		"Title":         "シナリオスタジオ (SSG)",
		"ActiveNav":     "scenarios",
		"ScenariosData": gin.H{"Scenarios": scenarios},
	}
	if renderErr := tmpl.ExecuteTemplate(scenariosFile, "layout", scenariosData); renderErr != nil {
		return fmt.Errorf("render scenarios.html: %w", renderErr)
	}

	return nil
}
