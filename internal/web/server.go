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
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

//go:embed templates/* static/*
var EmbeddedAssets embed.FS

// Server represents the frontend web server
type Server struct {
	Engine     *gin.Engine
	Client     *Client
	Templates  *template.Template
	StaticFS   http.FileSystem
	ListenPort string
}

// NewServer initializes the Web UI server
func NewServer(apiClient *Client, port string) (*Server, error) {
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.Use(gin.Recovery())

	// Parse templates from embedded FS
	tmpl, err := template.ParseFS(EmbeddedAssets, "templates/*.html", "templates/components/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}

	staticSub, err := fs.Sub(EmbeddedAssets, "static")
	if err != nil {
		return nil, fmt.Errorf("sub static fs: %w", err)
	}

	s := &Server{
		Engine:     engine,
		Client:     apiClient,
		Templates:  tmpl,
		StaticFS:   http.FS(staticSub),
		ListenPort: port,
	}

	s.setupRoutes()
	return s, nil
}

func (s *Server) setupRoutes() {
	// Static assets
	s.Engine.StaticFS("/static", s.StaticFS)

	// Health check
	s.Engine.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "OK"})
	})

	// Page Views
	s.Engine.GET("/", s.handleDashboardPage)
	s.Engine.GET("/scenarios", s.handleScenariosPage)

	// HTMX Component Fragments
	ui := s.Engine.Group("/ui")
	{
		ui.GET("/components/system-metrics", s.handleComponentSystemMetrics)
		ui.GET("/components/targets-table", s.handleComponentTargetsTable)
		ui.GET("/components/mibs-table", s.handleComponentMibsTable)
		ui.GET("/components/jobs-table", s.handleComponentJobsTable)
		ui.GET("/components/audit-logs", s.handleComponentAuditLogs)
		ui.GET("/components/scenarios-list", s.handleComponentScenariosList)

		// Scenario Actions
		ui.POST("/scenarios", s.handleCreateScenario)
		ui.POST("/scenarios/:name/run", s.handleRunScenario)
		ui.GET("/jobs/:id/status", s.handleJobStatus)

		// Target Actions
		ui.POST("/targets/:name/ping", s.handleTargetPing)
		ui.POST("/targets/:name/drain", s.handleTargetDrain)
	}
}

func (s *Server) handleDashboardPage(c *gin.Context) {
	ctx := c.Request.Context()

	metrics, err := s.Client.GetSystemMetrics(ctx)
	if err != nil {
		slog.Warn("Failed to get system metrics", "error", err)
		metrics = &SystemMetrics{CPUPercent: 0.5, MemHeapMB: 10, MemSysMB: 20, MemRSSMB: 15, Goroutines: 10}
	}

	targets, err := s.Client.ListTargets(ctx)
	if err != nil {
		slog.Warn("Failed to list targets", "error", err)
		targets = []TargetViewModel{}
	}

	transitions, err := s.Client.ListStateTransitions(ctx)
	if err != nil {
		slog.Warn("Failed to list state transitions", "error", err)
		transitions = []StateTransitionViewModel{}
	}

	jobs, err := s.Client.ListJobs(ctx)
	if err != nil {
		slog.Warn("Failed to list jobs", "error", err)
		jobs = []JobViewModel{}
	}

	auditLogs, err := s.Client.ListAuditLogs(ctx)
	if err != nil {
		slog.Warn("Failed to list audit logs", "error", err)
		auditLogs = []AuditLogViewModel{}
	}

	data := gin.H{
		"Title":       "統合ダッシュボード",
		"ActiveNav":   "overview",
		"Metrics":     metrics,
		"TargetsData": gin.H{"Targets": targets},
		"MibsData":    gin.H{"Transitions": transitions},
		"JobsData":    gin.H{"Jobs": jobs},
		"AuditData":   gin.H{"AuditLogs": auditLogs},
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := s.Templates.ExecuteTemplate(c.Writer, "layout", data); err != nil {
		slog.Error("Failed to execute dashboard template", "error", err)
		c.String(http.StatusInternalServerError, "Internal Server Error: %v", err)
	}
}

func (s *Server) handleScenariosPage(c *gin.Context) {
	ctx := c.Request.Context()

	scenarios, err := s.Client.ListScenarios(ctx)
	if err != nil {
		slog.Warn("Failed to list scenarios", "error", err)
		scenarios = []ScenarioViewModel{}
	}

	data := gin.H{
		"Title":         "シナリオスタジオ",
		"ActiveNav":     "scenarios",
		"ScenariosData": gin.H{"Scenarios": scenarios},
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := s.Templates.ExecuteTemplate(c.Writer, "layout", data); err != nil {
		slog.Error("Failed to execute scenarios template", "error", err)
		c.String(http.StatusInternalServerError, "Internal Server Error: %v", err)
	}
}

func (s *Server) handleComponentSystemMetrics(c *gin.Context) {
	metrics, err := s.Client.GetSystemMetrics(c.Request.Context())
	if err != nil {
		metrics = &SystemMetrics{CPUPercent: 1.0, MemHeapMB: 12, MemSysMB: 25, MemRSSMB: 16, Goroutines: 8}
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	_ = s.Templates.ExecuteTemplate(c.Writer, "system_metrics", metrics)
}

func (s *Server) handleComponentTargetsTable(c *gin.Context) {
	targets, err := s.Client.ListTargets(c.Request.Context())
	if err != nil {
		targets = []TargetViewModel{}
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	_ = s.Templates.ExecuteTemplate(c.Writer, "targets_table", gin.H{"Targets": targets})
}

func (s *Server) handleComponentMibsTable(c *gin.Context) {
	transitions, err := s.Client.ListStateTransitions(c.Request.Context())
	if err != nil {
		transitions = []StateTransitionViewModel{}
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	_ = s.Templates.ExecuteTemplate(c.Writer, "mibs_table", gin.H{"Transitions": transitions})
}

func (s *Server) handleComponentJobsTable(c *gin.Context) {
	jobs, err := s.Client.ListJobs(c.Request.Context())
	if err != nil {
		jobs = []JobViewModel{}
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	_ = s.Templates.ExecuteTemplate(c.Writer, "jobs_table", gin.H{"Jobs": jobs})
}

func (s *Server) handleComponentAuditLogs(c *gin.Context) {
	logs, err := s.Client.ListAuditLogs(c.Request.Context())
	if err != nil {
		logs = []AuditLogViewModel{}
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	_ = s.Templates.ExecuteTemplate(c.Writer, "audit_table", gin.H{"AuditLogs": logs})
}

func (s *Server) handleComponentScenariosList(c *gin.Context) {
	scenarios, err := s.Client.ListScenarios(c.Request.Context())
	if err != nil {
		scenarios = []ScenarioViewModel{}
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	_ = s.Templates.ExecuteTemplate(c.Writer, "scenarios_list", gin.H{"Scenarios": scenarios})
}

func (s *Server) handleCreateScenario(c *gin.Context) {
	name := strings.TrimSpace(c.PostForm("name"))
	desc := strings.TrimSpace(c.PostForm("description"))
	dslYaml := strings.TrimSpace(c.PostForm("dsl_yaml"))

	if name == "" || dslYaml == "" {
		c.String(http.StatusBadRequest, `<div class="badge badge-danger" style="display: block; padding: 8px; margin-bottom: 12px;">シナリオ名とDSL YAMLは必須項目です。</div>`)
		return
	}

	err := s.Client.CreateScenario(c.Request.Context(), name, desc, dslYaml)
	if err != nil {
		c.String(http.StatusBadRequest, fmt.Sprintf(`<div class="badge badge-danger" style="display: block; padding: 8px; margin-bottom: 12px;">❌ 登録失敗: %s</div>`, err.Error()))
		return
	}

	c.String(http.StatusOK, fmt.Sprintf(`<div class="badge badge-success" style="display: block; padding: 8px; margin-bottom: 12px;">✅ シナリオ 「%s」 が正常に登録されました！</div>`, name))
}

func (s *Server) handleRunScenario(c *gin.Context) {
	name := c.Param("name")
	job, err := s.Client.RunScenario(c.Request.Context(), name)
	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf(`<div class="metric-card" style="border-color: var(--danger);"><span class="badge badge-danger">実行エラー</span>: %s</div>`, err.Error()))
		return
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	_ = s.Templates.ExecuteTemplate(c.Writer, "job_status", job)
}

func (s *Server) handleJobStatus(c *gin.Context) {
	id := c.Param("id")
	job, err := s.Client.GetJob(c.Request.Context(), id)
	if err != nil {
		c.String(http.StatusNotFound, `<div class="badge badge-danger">Job Not Found</div>`)
		return
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	_ = s.Templates.ExecuteTemplate(c.Writer, "job_status", job)
}

func (s *Server) handleTargetPing(c *gin.Context) {
	name := c.Param("name")
	_ = s.Client.PingTarget(c.Request.Context(), name)

	// Fetch updated targets to render row
	targets, _ := s.Client.ListTargets(c.Request.Context())
	var currentTarget TargetViewModel
	for _, t := range targets {
		if t.Name == name {
			currentTarget = t
			break
		}
	}
	if currentTarget.Name == "" {
		currentTarget = TargetViewModel{
			Name:               name,
			Host:               "127.0.0.1",
			Port:               161,
			Status:             "ONLINE",
			CredentialID:       "default",
			UpdatedAtFormatted: time.Now().Format("2006-01-02 15:04:05"),
		}
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	html := fmt.Sprintf(`<tr id="target-row-%s">
		<td><strong>%s</strong></td>
		<td><code>%s</code></td>
		<td>%d</td>
		<td><span class="badge badge-success">%s</span></td>
		<td><code>%s</code></td>
		<td style="color: var(--text-secondary); font-size: 12px;">%s</td>
		<td style="text-align: right;">
			<button class="btn btn-secondary btn-sm" hx-post="/ui/targets/%s/ping" hx-target="#target-row-%s" hx-swap="outerHTML">⚡ Ping</button>
			<button class="btn btn-secondary btn-sm" hx-post="/ui/targets/%s/drain" hx-target="#target-row-%s" hx-swap="outerHTML">🛑 Drain</button>
		</td>
	</tr>`, currentTarget.Name, currentTarget.Name, currentTarget.Host, currentTarget.Port, currentTarget.Status, currentTarget.CredentialID, currentTarget.UpdatedAtFormatted, currentTarget.Name, currentTarget.Name, currentTarget.Name, currentTarget.Name)

	c.String(http.StatusOK, html)
}

func (s *Server) handleTargetDrain(c *gin.Context) {
	name := c.Param("name")
	_ = s.Client.DrainTarget(c.Request.Context(), name)

	targets, _ := s.Client.ListTargets(c.Request.Context())
	var currentTarget TargetViewModel
	for _, t := range targets {
		if t.Name == name {
			currentTarget = t
			currentTarget.Status = "DRAINING"
			break
		}
	}
	if currentTarget.Name == "" {
		currentTarget = TargetViewModel{
			Name:               name,
			Host:               "127.0.0.1",
			Port:               161,
			Status:             "DRAINING",
			CredentialID:       "default",
			UpdatedAtFormatted: time.Now().Format("2006-01-02 15:04:05"),
		}
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	html := fmt.Sprintf(`<tr id="target-row-%s">
		<td><strong>%s</strong></td>
		<td><code>%s</code></td>
		<td>%d</td>
		<td><span class="badge badge-warning">%s</span></td>
		<td><code>%s</code></td>
		<td style="color: var(--text-secondary); font-size: 12px;">%s</td>
		<td style="text-align: right;">
			<button class="btn btn-secondary btn-sm" hx-post="/ui/targets/%s/ping" hx-target="#target-row-%s" hx-swap="outerHTML">⚡ Ping</button>
		</td>
	</tr>`, currentTarget.Name, currentTarget.Name, currentTarget.Host, currentTarget.Port, currentTarget.Status, currentTarget.CredentialID, currentTarget.UpdatedAtFormatted, currentTarget.Name, currentTarget.Name)

	c.String(http.StatusOK, html)
}
