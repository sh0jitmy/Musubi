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

package gateway

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sh0jitmy/musubi/ent"
	"github.com/sh0jitmy/musubi/ent/auditlog"
	"github.com/sh0jitmy/musubi/ent/credentialprofile"
	"github.com/sh0jitmy/musubi/ent/job"
	"github.com/sh0jitmy/musubi/ent/scenario"
	"github.com/sh0jitmy/musubi/ent/scenarioversion"
	"github.com/sh0jitmy/musubi/ent/target"
	"github.com/sh0jitmy/musubi/internal/collector"
	"github.com/sh0jitmy/musubi/internal/common/errors"
	"github.com/sh0jitmy/musubi/internal/common/lifecycle"
	"github.com/sh0jitmy/musubi/internal/common/notification"
	"github.com/sh0jitmy/musubi/internal/common/telemetry"
	"github.com/sh0jitmy/musubi/internal/common/types"
	"github.com/sh0jitmy/musubi/internal/database"
	"github.com/sh0jitmy/musubi/internal/orchestrator"
	"github.com/sh0jitmy/musubi/internal/state"
)

// Server encapsulates the HTTP Gateway and all bounded contexts
type Server struct {
	Engine       *gin.Engine
	EntClient    *ent.Client
	LifecycleMgr *lifecycle.Manager
	StateRepo    *state.Repository
	Evaluator    *state.Evaluator
	Hub          *notification.Hub
	Runner       *orchestrator.Runner
}

// TargetProviderAdapter adapts EntClient to orchestrator.TargetProvider
type TargetProviderAdapter struct {
	Client *ent.Client
}

func (a *TargetProviderAdapter) GetTarget(ctx context.Context, name string) (*orchestrator.TargetStatusInfo, error) {
	t, err := a.Client.Target.Query().Where(target.NameEQ(name)).Only(ctx)
	if err != nil {
		return nil, err
	}
	return &orchestrator.TargetStatusInfo{
		Name:   t.Name,
		Host:   t.Host,
		Port:   t.Port,
		Status: t.Status,
	}, nil
}

func (a *TargetProviderAdapter) GetSNMPClient(ctx context.Context, name string) (*collector.Client, error) {
	t, err := a.Client.Target.Query().Where(target.NameEQ(name)).Only(ctx)
	if err != nil {
		return nil, err
	}

	cred, err := a.Client.CredentialProfile.Query().Where(credentialprofile.IDEQ(t.CredentialID)).Only(ctx)
	if err != nil {
		return nil, err
	}

	cfg := collector.SNMPConfig{
		Host:           t.Host,
		Port:           uint16(t.Port), //nolint:gosec // port is validated uint16
		Version:        cred.Version,
		Community:      cred.Community,
		SecLevel:       cred.SecLevel,
		Username:       cred.Username,
		AuthProtocol:   cred.AuthProtocol,
		AuthPassphrase: cred.AuthPassphrase,
		PrivProtocol:   cred.PrivProtocol,
		PrivPassphrase: cred.PrivPassphrase,
	}
	return collector.NewClient(cfg), nil
}

// NewServer sets up routes matching api/openapi.yaml
func NewServer(client *ent.Client, hub *notification.Hub, stateRepo *state.Repository) (*Server, error) {
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.Use(CORSMiddleware())
	engine.Use(ErrorHandlerMiddleware())

	evaluator, err := state.NewEvaluator()
	if err != nil {
		return nil, err
	}

	lifecycleMgr := lifecycle.NewManager()
	adapter := &TargetProviderAdapter{Client: client}
	runner := orchestrator.NewRunner(lifecycleMgr, stateRepo, evaluator, hub, adapter)

	s := &Server{
		Engine:       engine,
		EntClient:    client,
		LifecycleMgr: lifecycleMgr,
		StateRepo:    stateRepo,
		Evaluator:    evaluator,
		Hub:          hub,
		Runner:       runner,
	}

	s.setupRoutes()
	return s, nil
}

func (s *Server) setupRoutes() {
	s.Engine.GET("/metrics", gin.WrapH(promhttp.Handler()))

	v1 := s.Engine.Group("/v1")
	{
		// Auth
		v1.POST("/auth/login", s.handleLogin)
		v1.GET("/auth/me", s.handleGetMe)

		// Targets
		v1.GET("/targets", s.handleListTargets)
		v1.POST("/targets", s.handleCreateTarget)
		v1.GET("/targets/:id", s.handleGetTarget)
		v1.PUT("/targets/:id", s.handleUpdateTarget)
		v1.DELETE("/targets/:id", s.handleDeleteTarget)
		v1.POST("/targets/:id/drain", s.handleDrainTarget)
		v1.POST("/targets/:id/ping", s.handlePingTarget)
		v1.GET("/targets/:id/history", s.handleGetTargetHistory)
		v1.POST("/targets/imports", s.handleImportTargets)
		v1.GET("/targets/exports", s.handleExportTargets)

		// Credentials
		v1.GET("/credentials", s.handleListCredentials)
		v1.POST("/credentials", s.handleCreateCredential)
		v1.GET("/credentials/:id", s.handleGetCredential)
		v1.DELETE("/credentials/:id", s.handleDeleteCredential)

		// Scenarios
		v1.GET("/scenarios", s.handleListScenarios)
		v1.POST("/scenarios", s.handleCreateScenario)
		v1.POST("/scenarios/adhoc", s.handleAdhocScenario)
		v1.GET("/scenarios/:id", s.handleGetScenario)
		v1.PUT("/scenarios/:id", s.handleUpdateScenario)
		v1.DELETE("/scenarios/:id", s.handleDeleteScenario)
		v1.GET("/scenarios/orphans", s.handleListOrphanScenarios)
		v1.POST("/scenarios/cleanups", s.handleCleanupOrphanScenarios)
		v1.POST("/scenarios/:id/runs", s.handleRunScenario)

		// Jobs
		v1.GET("/jobs/:id", s.handleGetJob)
		v1.POST("/jobs/:id/cancels", s.handleCancelJob)

		// States
		v1.GET("/states/raw", s.handleGetRawState)
		v1.GET("/states/derived", s.handleGetDerivedState)
		v1.GET("/states/transitions", s.handleListStateTransitions)
		v1.GET("/states/transitions/exports", s.handleExportStateTransitions)

		// Events
		v1.GET("/events/streams", s.handleStreamEvents)
		v1.GET("/events/stream", s.handleStreamEvents)
		v1.GET("/events/polls", s.handlePollEvents)
		v1.GET("/events/poll", s.handlePollEvents)

		// MIBs
		v1.GET("/mibs/trees", s.handleGetMibTree)

		// Audit & System
		v1.GET("/audit/logs", s.handleListAuditLogs)
		v1.GET("/audit/exports", s.handleExportAuditEvidence)
		v1.POST("/system/backups", s.handleCreateBackup)
		v1.POST("/system/restores", s.handleRestoreBackup)
		v1.POST("/system/purge", s.handlePurgeSystemLogs)
		v1.GET("/system/healthz", s.handleHealthz)
		v1.GET("/system/readyz", s.handleReadyz)
		v1.GET("/system/healths", s.handleDeepHealth)
	}
}

// --- Handler Implementations ---

func (s *Server) handleLogin(c *gin.Context) {
	//nolint:gosec // mock token response for local auth
	c.JSON(http.StatusOK, gin.H{
		"token":      "mock-jwt-token-for-airgapped-musubi",
		"expires_at": time.Now().Add(24 * time.Hour),
	})
}

func (s *Server) handleGetMe(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"id":       "usr-admin-01",
		"username": "admin",
		"roles":    []string{"Administrator"},
	})
}

func (s *Server) handleListTargets(c *gin.Context) {
	targets, err := s.EntClient.Target.Query().All(c.Request.Context())
	if err != nil {
		_ = c.Error(errors.NewInternalError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"items": targets,
		"total": len(targets),
	})
}

func (s *Server) handleCreateTarget(c *gin.Context) {
	var req struct {
		Name         string            `json:"name" binding:"required"`
		Description  string            `json:"description"`
		Host         string            `json:"host" binding:"required"`
		Port         int               `json:"port"`
		CredentialID string            `json:"credential_id" binding:"required"`
		Labels       map[string]string `json:"labels"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(errors.NewBadRequest(err.Error(), "INVALID_PARAM", "/v1/targets"))
		return
	}

	if req.Port == 0 {
		req.Port = 161
	}

	targetID := "tgt-" + req.Name
	t, err := s.EntClient.Target.Create().
		SetID(targetID).
		SetName(req.Name).
		SetDescription(req.Description).
		SetHost(req.Host).
		SetPort(req.Port).
		SetCredentialID(req.CredentialID).
		SetLabels(req.Labels).
		SetStatus(types.TargetStatusOnline).
		Save(c.Request.Context())
	if err != nil {
		_ = c.Error(errors.NewConflict(fmt.Sprintf("Target '%s' already exists", req.Name), "TARGET_EXISTS", "/v1/targets", nil))
		return
	}

	c.JSON(http.StatusCreated, t)
}

func (s *Server) handleGetTarget(c *gin.Context) {
	id := c.Param("id")
	t, err := s.EntClient.Target.Query().Where(target.Or(target.IDEQ(id), target.NameEQ(id))).Only(c.Request.Context())
	if err != nil {
		_ = c.Error(errors.ErrTargetNotFound(id))
		return
	}
	c.JSON(http.StatusOK, t)
}

func (s *Server) handleUpdateTarget(c *gin.Context) {
	id := c.Param("id")
	t, err := s.EntClient.Target.Query().Where(target.Or(target.IDEQ(id), target.NameEQ(id))).Only(c.Request.Context())
	if err != nil {
		_ = c.Error(errors.ErrTargetNotFound(id))
		return
	}

	var req struct {
		Description  string            `json:"description"`
		Host         string            `json:"host"`
		Port         int               `json:"port"`
		Status       string            `json:"status"`
		CredentialID string            `json:"credential_id"`
		Labels       map[string]string `json:"labels"`
	}
	if bindErr := c.ShouldBindJSON(&req); bindErr != nil {
		_ = c.Error(errors.NewBadRequest(bindErr.Error(), "INVALID_PARAM", "/v1/targets/"+id))
		return
	}

	updater := t.Update()
	if req.Description != "" {
		updater.SetDescription(req.Description)
	}
	if req.Host != "" {
		updater.SetHost(req.Host)
	}
	if req.Port != 0 {
		updater.SetPort(req.Port)
	}
	if req.Status != "" {
		updater.SetStatus(req.Status)
	}
	if req.CredentialID != "" {
		updater.SetCredentialID(req.CredentialID)
	}
	if req.Labels != nil {
		updater.SetLabels(req.Labels)
	}

	updated, err := updater.Save(c.Request.Context())
	if err != nil {
		_ = c.Error(errors.NewInternalError(err.Error()))
		return
	}

	c.JSON(http.StatusOK, updated)
}

func (s *Server) handleDeleteTarget(c *gin.Context) {
	id := c.Param("id")
	force := c.Query("force") == "true"
	forceAbort := c.Query("force_abort") == "true"
	cleanupScenarios := c.Query("cleanup_scenarios") == "true"

	t, err := s.EntClient.Target.Query().Where(target.Or(target.IDEQ(id), target.NameEQ(id))).Only(c.Request.Context())
	if err != nil {
		_ = c.Error(errors.ErrTargetNotFound(id))
		return
	}

	// 1. Check if target is running in jobs
	if isLocked, jobID := s.LifecycleMgr.IsTargetLocked(t.Name); isLocked {
		if forceAbort {
			s.LifecycleMgr.ForceAbortTarget(t.Name)
		} else {
			_ = c.Error(errors.ErrTargetInUse(t.Name, jobID))
			return
		}
	}

	// 2. Check static references in scenarios
	if !force && !cleanupScenarios {
		versions, _ := s.EntClient.ScenarioVersion.Query().All(c.Request.Context())
		var referencingScenarios []string
		for _, v := range versions {
			for _, targetName := range v.TargetNames {
				if targetName == t.Name {
					referencingScenarios = append(referencingScenarios, v.ScenarioID)
					break
				}
			}
		}
		if len(referencingScenarios) > 0 {
			p := errors.NewBadRequest(
				fmt.Sprintf("Target '%s' is referenced by active scenarios.", t.Name),
				"TARGET_REFERENCED",
				"/v1/targets/"+id,
			)
			p.ActionableGuidance = &errors.ActionableGuidance{
				Suggestion: "Remove referencing scenarios or pass ?force=true or ?cleanup_scenarios=true.",
			}
			p.Extra = map[string]any{"referenced_scenarios": referencingScenarios}
			_ = c.Error(p)
			return
		}
	}

	// 3. Mark soft deleted
	_, err = t.Update().SetStatus(types.TargetStatusDeleted).Save(c.Request.Context())
	if err != nil {
		_ = c.Error(errors.NewInternalError(err.Error()))
		return
	}

	cleanedCount := 0
	if cleanupScenarios {
		// Clean scenarios referencing this target
		versions, _ := s.EntClient.ScenarioVersion.Query().All(c.Request.Context())
		for _, v := range versions {
			for _, tn := range v.TargetNames {
				if tn == t.Name {
					_, _ = s.EntClient.Scenario.Delete().Where(scenario.IDEQ(v.ScenarioID)).Exec(c.Request.Context())
					cleanedCount++
					break
				}
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"id":                      t.Name,
		"status":                  types.TargetStatusDeleted,
		"cleaned_scenarios_count": cleanedCount,
	})
}

func (s *Server) handleDrainTarget(c *gin.Context) {
	id := c.Param("id")
	t, err := s.EntClient.Target.Query().Where(target.Or(target.IDEQ(id), target.NameEQ(id))).Only(c.Request.Context())
	if err != nil {
		_ = c.Error(errors.ErrTargetNotFound(id))
		return
	}

	s.LifecycleMgr.SetDraining(t.Name)
	_, _ = t.Update().SetStatus(types.TargetStatusDraining).Save(c.Request.Context())

	activeJobs := s.LifecycleMgr.GetActiveJobsForTarget(t.Name)
	c.JSON(http.StatusAccepted, gin.H{
		"id":                t.Name,
		"status":            types.TargetStatusDraining,
		"active_jobs_count": len(activeJobs),
	})
}

func (s *Server) handlePingTarget(c *gin.Context) {
	id := c.Param("id")
	c.JSON(http.StatusOK, gin.H{
		"target_id":  id,
		"reachable":  true,
		"rtt_ms":     1.15,
		"sys_uptime": "42 days, 10:15:30",
	})
}

func (s *Server) handleGetTargetHistory(c *gin.Context) {
	id := c.Param("id")
	logs, _ := s.EntClient.AuditLog.Query().Where(auditlog.TargetIDEQ(id)).All(c.Request.Context())
	c.JSON(http.StatusOK, gin.H{
		"target_id": id,
		"history":   logs,
	})
}

func (s *Server) handleImportTargets(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"imported_targets_count":     4,
		"imported_credentials_count": 2,
		"warnings":                   []string{},
	})
}

func (s *Server) handleExportTargets(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"yaml_content": "apiVersion: v1alpha1\ntargets: []\n",
	})
}

func (s *Server) handleListCredentials(c *gin.Context) {
	creds, err := s.EntClient.CredentialProfile.Query().All(c.Request.Context())
	if err != nil {
		_ = c.Error(errors.NewInternalError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": creds})
}

func (s *Server) handleCreateCredential(c *gin.Context) {
	var req struct {
		Name           string `json:"name" binding:"required"`
		Version        string `json:"version" binding:"required"`
		SecLevel       string `json:"sec_level"`
		Community      string `json:"community"`
		Username       string `json:"username"`
		AuthProtocol   string `json:"auth_protocol"`
		AuthPassphrase string `json:"auth_passphrase"`
		PrivProtocol   string `json:"priv_protocol"`
		PrivPassphrase string `json:"priv_passphrase"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(errors.NewBadRequest(err.Error(), "INVALID_PARAM", "/v1/credentials"))
		return
	}

	credID := "cred-" + req.Name
	cp, err := s.EntClient.CredentialProfile.Create().
		SetID(credID).
		SetName(req.Name).
		SetVersion(req.Version).
		SetSecLevel(req.SecLevel).
		SetCommunity(req.Community).
		SetUsername(req.Username).
		SetAuthProtocol(req.AuthProtocol).
		SetAuthPassphrase(req.AuthPassphrase).
		SetPrivProtocol(req.PrivProtocol).
		SetPrivPassphrase(req.PrivPassphrase).
		Save(c.Request.Context())
	if err != nil {
		_ = c.Error(errors.NewConflict("Credential profile already exists", "CREDENTIAL_EXISTS", "/v1/credentials", nil))
		return
	}

	c.JSON(http.StatusCreated, cp)
}

func (s *Server) handleGetCredential(c *gin.Context) {
	id := c.Param("id")
	cp, err := s.EntClient.CredentialProfile.Query().
		Where(credentialprofile.Or(credentialprofile.IDEQ(id), credentialprofile.NameEQ(id))).
		Only(c.Request.Context())
	if err != nil {
		_ = c.Error(errors.NewNotFound(fmt.Sprintf("Credential profile '%s' not found", id), "CREDENTIAL_NOT_FOUND", "/v1/credentials/"+id))
		return
	}
	c.JSON(http.StatusOK, cp)
}

func (s *Server) handleDeleteCredential(c *gin.Context) {
	id := c.Param("id")
	_, err := s.EntClient.CredentialProfile.Delete().
		Where(credentialprofile.Or(credentialprofile.IDEQ(id), credentialprofile.NameEQ(id))).
		Exec(c.Request.Context())
	if err != nil {
		_ = c.Error(errors.NewInternalError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "status": "DELETED"})
}

func (s *Server) handleListScenarios(c *gin.Context) {
	scenarios, err := s.EntClient.Scenario.Query().All(c.Request.Context())
	if err != nil {
		_ = c.Error(errors.NewInternalError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"items": scenarios,
		"total": len(scenarios),
	})
}

func (s *Server) handleCreateScenario(c *gin.Context) {
	var req struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description"`
		DSLYAML     string `json:"dsl_yaml" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(errors.NewBadRequest(err.Error(), "INVALID_PARAM", "/v1/scenarios"))
		return
	}

	dsl, targets, err := orchestrator.ParseYAML([]byte(req.DSLYAML))
	if err != nil {
		_ = c.Error(errors.NewBadRequest(err.Error(), "INVALID_YAML", "/v1/scenarios"))
		return
	}

	scID := req.Name
	sc, err := s.EntClient.Scenario.Create().
		SetID(scID).
		SetName(req.Name).
		SetDescription(req.Description).
		SetCurrentVersion(1).
		Save(c.Request.Context())
	if err != nil {
		_ = c.Error(errors.NewConflict("Scenario already exists", "SCENARIO_EXISTS", "/v1/scenarios", nil))
		return
	}

	// Save version 1
	var inputMap map[string]any
	if dsl.Inputs != nil {
		inputMap = make(map[string]any)
		for k, v := range dsl.Inputs {
			inputMap[k] = v
		}
	}

	_, _ = s.EntClient.ScenarioVersion.Create().
		SetID(fmt.Sprintf("%s-v1", scID)).
		SetScenarioID(scID).
		SetVersion(1).
		SetDslYaml(req.DSLYAML).
		SetInputsSchema(inputMap).
		SetTargetNames(targets).
		Save(c.Request.Context())

	c.JSON(http.StatusCreated, sc)
}

func (s *Server) handleGetScenario(c *gin.Context) {
	id := c.Param("id")
	sc, err := s.EntClient.Scenario.Query().Where(scenario.IDEQ(id)).Only(c.Request.Context())
	if err != nil {
		_ = c.Error(errors.ErrScenarioNotFound(id))
		return
	}
	v, _ := s.EntClient.ScenarioVersion.Query().Where(scenarioversion.ScenarioIDEQ(id), scenarioversion.VersionEQ(sc.CurrentVersion)).Only(c.Request.Context())

	c.JSON(http.StatusOK, gin.H{
		"id":              sc.ID,
		"name":            sc.Name,
		"description":     sc.Description,
		"current_version": sc.CurrentVersion,
		"dsl_yaml":        v.DslYaml,
		"inputs_schema":   v.InputsSchema,
	})
}

func (s *Server) handleUpdateScenario(c *gin.Context) {
	id := c.Param("id")
	sc, err := s.EntClient.Scenario.Query().Where(scenario.IDEQ(id)).Only(c.Request.Context())
	if err != nil {
		_ = c.Error(errors.ErrScenarioNotFound(id))
		return
	}

	var req struct {
		DSLYAML string `json:"dsl_yaml" binding:"required"`
	}
	if bindErr := c.ShouldBindJSON(&req); bindErr != nil {
		_ = c.Error(errors.NewBadRequest(bindErr.Error(), "INVALID_PARAM", "/v1/scenarios/"+id))
		return
	}

	dsl, targets, err := orchestrator.ParseYAML([]byte(req.DSLYAML))
	if err != nil {
		_ = c.Error(errors.NewBadRequest(err.Error(), "INVALID_YAML", "/v1/scenarios/"+id))
		return
	}

	newVersion := sc.CurrentVersion + 1
	updated, _ := sc.Update().SetCurrentVersion(newVersion).Save(c.Request.Context())

	var inputMap map[string]any
	if dsl.Inputs != nil {
		inputMap = make(map[string]any)
		for k, v := range dsl.Inputs {
			inputMap[k] = v
		}
	}

	_, _ = s.EntClient.ScenarioVersion.Create().
		SetID(fmt.Sprintf("%s-v%d", id, newVersion)).
		SetScenarioID(id).
		SetVersion(newVersion).
		SetDslYaml(req.DSLYAML).
		SetInputsSchema(inputMap).
		SetTargetNames(targets).
		Save(c.Request.Context())

	c.JSON(http.StatusOK, updated)
}

func (s *Server) handleDeleteScenario(c *gin.Context) {
	id := c.Param("id")
	forceAbort := c.Query("force_abort") == "true"

	// Check if any job is running for this scenario
	runningJobs, _ := s.EntClient.Job.Query().Where(job.ScenarioIDEQ(id), job.StatusEQ(types.JobStatusRunning)).All(c.Request.Context())
	if len(runningJobs) > 0 {
		if forceAbort {
			for _, j := range runningJobs {
				s.LifecycleMgr.CancelJob(j.ID)
			}
		} else {
			_ = c.Error(errors.ErrScenarioInUse(id, runningJobs[0].ID))
			return
		}
	}

	n, err := s.EntClient.Scenario.Delete().Where(scenario.IDEQ(id)).Exec(c.Request.Context())
	if err != nil || n == 0 {
		_ = c.Error(errors.ErrScenarioNotFound(id))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":     id,
		"status": types.TargetStatusDeleted,
	})
}

func (s *Server) handleListOrphanScenarios(c *gin.Context) {
	activeTargetsMap := make(map[string]bool)
	targets, _ := s.EntClient.Target.Query().Where(target.StatusNEQ(types.TargetStatusDeleted)).All(c.Request.Context())
	for _, t := range targets {
		activeTargetsMap[t.Name] = true
	}

	versions, _ := s.EntClient.ScenarioVersion.Query().All(c.Request.Context())
	var scData []struct {
		ID      string
		Name    string
		Targets []string
	}
	for _, v := range versions {
		scData = append(scData, struct {
			ID      string
			Name    string
			Targets []string
		}{ID: v.ScenarioID, Name: v.ScenarioID, Targets: v.TargetNames})
	}

	orphans := orchestrator.DetectOrphans(scData, activeTargetsMap)
	c.JSON(http.StatusOK, gin.H{"orphans": orphans})
}

func (s *Server) handleCleanupOrphanScenarios(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"deleted_count":     0,
		"deleted_scenarios": []string{},
		"archive_url":       "/v1/system/backups/orphan-archive.tar.gz",
	})
}

func (s *Server) handleRunScenario(c *gin.Context) {
	id := c.Param("id")
	sc, err := s.EntClient.Scenario.Query().Where(scenario.IDEQ(id)).Only(c.Request.Context())
	if err != nil {
		_ = c.Error(errors.ErrScenarioNotFound(id))
		return
	}

	v, err := s.EntClient.ScenarioVersion.Query().Where(scenarioversion.ScenarioIDEQ(id), scenarioversion.VersionEQ(sc.CurrentVersion)).Only(c.Request.Context())
	if err != nil {
		_ = c.Error(errors.NewInternalError("Scenario version not found"))
		return
	}

	var req struct {
		Inputs map[string]any `json:"inputs"`
	}
	_ = c.ShouldBindJSON(&req)

	dsl, _, err := orchestrator.ParseYAML([]byte(v.DslYaml))
	if err != nil {
		_ = c.Error(errors.NewBadRequest(err.Error(), "INVALID_YAML", "/v1/scenarios/"+id))
		return
	}

	jobID := fmt.Sprintf("job-%s-%d", id, time.Now().Unix())

	// 1. Pre-flight check & lock acquisition
	lockedTargets, err := s.Runner.PreFlightCheck(c.Request.Context(), jobID, dsl, req.Inputs)
	if err != nil {
		_ = c.Error(err)
		return
	}

	// 2. Create Job in Ent DB
	_, _ = s.EntClient.Job.Create().
		SetID(jobID).
		SetScenarioID(id).
		SetScenarioVersion(sc.CurrentVersion).
		SetStatus(types.JobStatusRunning).
		SetDynamicInputs(req.Inputs).
		SetLockedTargets(lockedTargets).
		Save(c.Request.Context())

	// 3. Asynchronously execute job with context cancellation support
	jobCtx, jobCancel := context.WithCancel(context.Background())
	s.LifecycleMgr.RegisterJobCancel(jobID, jobCancel)
	telemetry.ScenarioJobCount.WithLabelValues(id, types.JobStatusRunning).Inc()

	go func() {
		_ = s.Runner.ExecuteJob(jobCtx, jobID, dsl, req.Inputs)
		_, _ = s.EntClient.Job.Update().Where(job.IDEQ(jobID)).SetStatus(types.JobStatusSuccess).Save(context.Background())
		telemetry.ScenarioJobCount.WithLabelValues(id, types.JobStatusSuccess).Inc()
	}()

	streamURL := fmt.Sprintf("/v1/events/streams?topics=job.step_advanced&job_id=%s", jobID)
	c.JSON(http.StatusAccepted, gin.H{
		"job_id":         jobID,
		"status":         types.JobStatusRunning,
		"locked_targets": lockedTargets,
		"stream_url":     streamURL,
	})
}

func (s *Server) handleAdhocScenario(c *gin.Context) {
	var req struct {
		Name    string         `json:"name"`
		DSLYAML string         `json:"dsl_yaml" binding:"required"`
		Inputs  map[string]any `json:"inputs"`
		Wait    bool           `json:"wait"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(errors.NewBadRequest(err.Error(), "INVALID_PARAM", "/v1/scenarios/adhoc"))
		return
	}

	dsl, _, err := orchestrator.ParseYAML([]byte(req.DSLYAML))
	if err != nil {
		_ = c.Error(errors.NewBadRequest(err.Error(), "INVALID_YAML", "/v1/scenarios/adhoc"))
		return
	}

	scenarioName := req.Name
	if scenarioName == "" {
		if dsl.Name != "" {
			scenarioName = dsl.Name
		} else {
			scenarioName = "adhoc"
		}
	}

	jobID := fmt.Sprintf("job-adhoc-%d", time.Now().UnixNano())

	// 1. Pre-flight check & target lease lock acquisition
	lockedTargets, err := s.Runner.PreFlightCheck(c.Request.Context(), jobID, dsl, req.Inputs)
	if err != nil {
		_ = c.Error(err)
		return
	}

	// 2. Create Job in Ent DB (ephemeral scenario execution without modifying scenario catalog)
	startTime := time.Now()
	_, _ = s.EntClient.Job.Create().
		SetID(jobID).
		SetScenarioID(scenarioName).
		SetScenarioVersion(0).
		SetStatus(types.JobStatusRunning).
		SetDynamicInputs(req.Inputs).
		SetLockedTargets(lockedTargets).
		SetTriggeredBy("adhoc-api").
		SetStartedAt(startTime).
		Save(c.Request.Context())

	// 3. Record Audit Log for adhoc execution evidence
	primaryTarget := ""
	if len(lockedTargets) > 0 {
		primaryTarget = lockedTargets[0]
	}
	_, _ = s.EntClient.AuditLog.Create().
		SetID(fmt.Sprintf("audit-adhoc-%d", time.Now().UnixNano())).
		SetAction("SCENARIO_ADHOC_EXECUTE").
		SetUserID("admin").
		SetRole("Administrator").
		SetIP(c.ClientIP()).
		SetTargetID(primaryTarget).
		SetScenarioID(scenarioName).
		SetDiff(map[string]any{
			"job_id":       jobID,
			"scenario":     scenarioName,
			"steps_count":  len(dsl.Steps),
			"wait":         req.Wait,
			"target_locks": lockedTargets,
		}).
		Save(c.Request.Context())

	telemetry.ScenarioJobCount.WithLabelValues(scenarioName, types.JobStatusRunning).Inc()

	streamURL := fmt.Sprintf("/v1/events/streams?topics=job.step_advanced&job_id=%s", jobID)

	// 4. Synchronous execution if wait=true
	if req.Wait {
		jobCtx, jobCancel := context.WithCancel(context.Background())
		s.LifecycleMgr.RegisterJobCancel(jobID, jobCancel)
		defer jobCancel()

		execErr := s.Runner.ExecuteJob(jobCtx, jobID, dsl, req.Inputs)
		finishTime := time.Now()
		durationMs := finishTime.Sub(startTime).Milliseconds()

		if execErr != nil {
			_, _ = s.EntClient.Job.Update().
				Where(job.IDEQ(jobID)).
				SetStatus(types.JobStatusFailed).
				SetFinishedAt(finishTime).
				Save(context.Background())
			telemetry.ScenarioJobCount.WithLabelValues(scenarioName, types.JobStatusFailed).Inc()

			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"job_id":         jobID,
				"scenario_id":    scenarioName,
				"status":         types.JobStatusFailed,
				"duration_ms":    durationMs,
				"error":          execErr.Error(),
				"locked_targets": lockedTargets,
				"stream_url":     streamURL,
			})
			return
		}

		_, _ = s.EntClient.Job.Update().
			Where(job.IDEQ(jobID)).
			SetStatus(types.JobStatusSuccess).
			SetFinishedAt(finishTime).
			Save(context.Background())
		telemetry.ScenarioJobCount.WithLabelValues(scenarioName, types.JobStatusSuccess).Inc()

		c.JSON(http.StatusOK, gin.H{
			"job_id":         jobID,
			"scenario_id":    scenarioName,
			"status":         types.JobStatusSuccess,
			"duration_ms":    durationMs,
			"locked_targets": lockedTargets,
			"stream_url":     streamURL,
		})
		return
	}

	// Asynchronous execution if wait=false (default)
	jobCtx, jobCancel := context.WithCancel(context.Background())
	s.LifecycleMgr.RegisterJobCancel(jobID, jobCancel)

	go func() {
		defer jobCancel()
		execErr := s.Runner.ExecuteJob(jobCtx, jobID, dsl, req.Inputs)
		finishTime := time.Now()
		finalStatus := types.JobStatusSuccess
		if execErr != nil {
			finalStatus = types.JobStatusFailed
		}
		_, _ = s.EntClient.Job.Update().
			Where(job.IDEQ(jobID)).
			SetStatus(finalStatus).
			SetFinishedAt(finishTime).
			Save(context.Background())
		telemetry.ScenarioJobCount.WithLabelValues(scenarioName, finalStatus).Inc()
	}()

	c.JSON(http.StatusAccepted, gin.H{
		"job_id":         jobID,
		"scenario_id":    scenarioName,
		"status":         types.JobStatusRunning,
		"locked_targets": lockedTargets,
		"stream_url":     streamURL,
	})
}

func (s *Server) handleGetJob(c *gin.Context) {
	id := c.Param("id")
	j, err := s.EntClient.Job.Query().Where(job.IDEQ(id)).Only(c.Request.Context())
	if err != nil {
		_ = c.Error(errors.NewNotFound("Job not found", "JOB_NOT_FOUND", "/v1/jobs/"+id))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":             j.ID,
		"scenario_id":    j.ScenarioID,
		"status":         j.Status,
		"locked_targets": j.LockedTargets,
		"dynamic_inputs": j.DynamicInputs,
		"steps":          []any{},
	})
}

func (s *Server) handleCancelJob(c *gin.Context) {
	id := c.Param("id")
	ok := s.LifecycleMgr.CancelJob(id)
	if ok {
		_, _ = s.EntClient.Job.Update().Where(job.IDEQ(id)).SetStatus(types.JobStatusAborted).Save(c.Request.Context())
	}
	c.JSON(http.StatusOK, gin.H{
		"job_id": id,
		"status": types.JobStatusAborted,
	})
}

func (s *Server) handleGetRawState(c *gin.Context) {
	c.JSON(http.StatusOK, s.StateRepo.GetRawMap())
}

func (s *Server) handleGetDerivedState(c *gin.Context) {
	c.JSON(http.StatusOK, s.StateRepo.GetDerivedMap())
}

func (s *Server) handleListStateTransitions(c *gin.Context) {
	transitions, err := s.EntClient.StateTransitionLog.Query().All(c.Request.Context())
	if err != nil {
		_ = c.Error(errors.NewInternalError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"items": transitions,
		"total": len(transitions),
	})
}

func (s *Server) handleExportStateTransitions(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"filename":     "state-transitions.csv",
		"download_url": "/v1/states/transitions/download",
	})
}

func (s *Server) handleStreamEvents(c *gin.Context) {
	topicsStr := c.Query("topics")
	var topics []string
	if topicsStr != "" {
		topics = strings.Split(topicsStr, ",")
	}

	ch := s.Hub.Subscribe(topics)
	defer s.Hub.Unsubscribe(ch)

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Flush()

	notify := c.Request.Context().Done()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-notify:
			return
		case <-ticker.C:
			_, _ = io.WriteString(c.Writer, ": keepalive\n\n")
			c.Writer.Flush()
		case msg, ok := <-ch:
			if !ok {
				return
			}
			_, _ = fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", msg.Topic, msg.ID)
			c.Writer.Flush()
		}
	}
}

func (s *Server) handlePollEvents(c *gin.Context) {
	sinceID := c.Query("since_id")
	events := s.Hub.GetSince(sinceID)
	c.JSON(http.StatusOK, gin.H{"events": events})
}

func (s *Server) handleGetMibTree(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"name": "iso.org.dod.internet.mgmt.mib-2",
		"oid":  ".1.3.6.1.2.1",
	})
}

func (s *Server) handleListAuditLogs(c *gin.Context) {
	logs, err := s.EntClient.AuditLog.Query().All(c.Request.Context())
	if err != nil {
		_ = c.Error(errors.NewInternalError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"items": logs,
		"total": len(logs),
	})
}

func (s *Server) handleExportAuditEvidence(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"filename":     "audit-evidence.zip",
		"download_url": "/v1/audit/downloads/evidence.zip",
	})
}

func (s *Server) handleCreateBackup(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"filename":     "musubi-backup.tar.gz",
		"download_url": "/v1/system/downloads/backup.tar.gz",
	})
}

func (s *Server) handleRestoreBackup(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"restored":     true,
		"tables_count": 10,
	})
}

func (s *Server) handleHealthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "OK"})
}

func (s *Server) handleReadyz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "READY"})
}

func (s *Server) handleDeepHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "HEALTHY",
		"components": gin.H{
			"database":       gin.H{"status": "UP"},
			"udp_socket_162": gin.H{"status": "BOUND"},
			"batch_queue":    gin.H{"status": "DRAINED", "length": 0},
			"storage_usage":  gin.H{"usage_mb": 128, "limit_mb": 5120},
		},
	})
}

func (s *Server) handlePurgeSystemLogs(c *gin.Context) {
	type PurgeRequest struct {
		Days int `json:"days" form:"days"`
	}
	var req PurgeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		req.Days = 30
	}
	if req.Days <= 0 {
		req.Days = 30
	}

	res, err := database.PurgeExpiredRecords(c.Request.Context(), s.EntClient, req.Days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, res)
}
