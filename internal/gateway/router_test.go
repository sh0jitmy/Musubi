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

package gateway_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "github.com/glebarez/go-sqlite"
	"github.com/sh0jitmy/musubi/internal/common/notification"
	"github.com/sh0jitmy/musubi/internal/database"
	"github.com/sh0jitmy/musubi/internal/gateway"
	"github.com/sh0jitmy/musubi/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func setupTestServer(t *testing.T, dbName string) *gateway.Server {
	t.Helper()
	client, err := database.NewClient(context.Background(), "sqlite3", "file:"+dbName+"?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = client.Close()
	})

	hub := notification.NewHub(100)
	stateRepo := state.NewRepository(nil)

	server, err := gateway.NewServer(client, hub, stateRepo)
	require.NoError(t, err)
	return server
}

func TestGateway_SystemAndAuth(t *testing.T) {
	t.Parallel()
	server := setupTestServer(t, "gw_sys")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/v1/system/healthz", nil)
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/v1/system/readyz", nil)
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "READY")

	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/v1/system/healths", nil)
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/metrics", nil)
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Auth Login
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader([]byte(`{"username":"admin","password":"password"}`)))
	req.Header.Set("Content-Type", "application/json")
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Auth Me
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGateway_CredentialsCRUD(t *testing.T) {
	t.Parallel()
	server := setupTestServer(t, "gw_creds")

	// Create
	payload := map[string]any{
		"name":      "v3-admin",
		"version":   "v3",
		"sec_level": "authPriv",
		"username":  "admin",
	}
	body, _ := json.Marshal(payload)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/v1/credentials", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)

	// List
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/v1/credentials", nil)
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Get Credential
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/v1/credentials/cred-v3-admin", nil)
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Delete Credential
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodDelete, "/v1/credentials/cred-v3-admin", nil)
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGateway_TargetsLifecycle(t *testing.T) {
	t.Parallel()
	server := setupTestServer(t, "gw_targets")

	// Create credential first
	credPayload := map[string]any{
		"name":      "v3-admin",
		"version":   "v3",
		"sec_level": "authPriv",
		"username":  "admin",
	}
	credBody, _ := json.Marshal(credPayload)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/v1/credentials", bytes.NewReader(credBody))
	req.Header.Set("Content-Type", "application/json")
	server.Engine.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	// Import / Export
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/v1/targets/imports", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/v1/targets/exports", nil)
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Create target
	//nolint:gosec // credential profile ID reference
	payload := map[string]any{
		"name":          "spine1",
		"host":          "192.168.10.1",
		"credential_id": "cred-v3-admin",
	}
	body, _ := json.Marshal(payload)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/v1/targets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)

	// Duplicate create error (409)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/v1/targets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusConflict, w.Code)

	// List targets
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/v1/targets", nil)
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Get target
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/v1/targets/spine1", nil)
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Update target
	upPayload := map[string]any{"port": 1620}
	upBody, _ := json.Marshal(upPayload)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPut, "/v1/targets/spine1", bytes.NewReader(upBody))
	req.Header.Set("Content-Type", "application/json")
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Ping target
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/v1/targets/spine1/ping", nil)
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Target history
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/v1/targets/spine1/history", nil)
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Drain target
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/v1/targets/spine1/drain", nil)
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusAccepted, w.Code)

	// Delete target
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodDelete, "/v1/targets/spine1?force=true", nil)
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGateway_ScenariosAndJobs(t *testing.T) {
	t.Parallel()
	server := setupTestServer(t, "gw_scenarios")

	// Create cred & target
	credPayload := map[string]any{
		"name":      "v3-admin",
		"version":   "v3",
		"sec_level": "authPriv",
		"username":  "admin",
	}
	credBody, _ := json.Marshal(credPayload)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/v1/credentials", bytes.NewReader(credBody))
	req.Header.Set("Content-Type", "application/json")
	server.Engine.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	//nolint:gosec // credential profile ID reference
	targetPayload := map[string]any{
		"name":          "spine1",
		"host":          "192.168.10.1",
		"credential_id": "cred-v3-admin",
	}
	tBody, _ := json.Marshal(targetPayload)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/v1/targets", bytes.NewReader(tBody))
	req.Header.Set("Content-Type", "application/json")
	server.Engine.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	yamlDSL := `
name: test-scenario
target_locks: [spine1]
steps:
  - id: s1
    target: spine1
`
	payload := map[string]any{
		"name":     "test-scenario",
		"dsl_yaml": yamlDSL,
	}
	body, _ := json.Marshal(payload)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/v1/scenarios", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)

	// Duplicate create error (409)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/v1/scenarios", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusConflict, w.Code)

	// List scenarios
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/v1/scenarios", nil)
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Get scenario
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/v1/scenarios/test-scenario", nil)
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Update scenario version
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPut, "/v1/scenarios/test-scenario", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Check orphans & Cleanup
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/v1/scenarios/orphans", nil)
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/v1/scenarios/cleanups", nil)
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// States endpoints
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/v1/states/raw", nil)
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/v1/states/derived", nil)
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/v1/states/transitions", nil)
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/v1/states/transitions/exports", nil)
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Create target spine2 for scenario run
	//nolint:gosec // credential profile ID reference
	p2 := map[string]any{"name": "spine2", "host": "192.168.10.2", "credential_id": "cred-v3-admin"}
	b2, _ := json.Marshal(p2)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/v1/targets", bytes.NewReader(b2))
	req.Header.Set("Content-Type", "application/json")
	server.Engine.ServeHTTP(w, req)

	// Create scenario targeting spine2
	sc2 := map[string]any{"name": "sc2", "dsl_yaml": "name: sc2\ntarget_locks: [spine2]\nsteps:\n  - id: s1\n    target: spine2\n"}
	bSc2, _ := json.Marshal(sc2)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/v1/scenarios", bytes.NewReader(bSc2))
	req.Header.Set("Content-Type", "application/json")
	server.Engine.ServeHTTP(w, req)

	// Run scenario
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/v1/scenarios/sc2/runs", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusAccepted, w.Code)

	var runRes struct {
		JobID string `json:"job_id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &runRes)

	// Get Job & Cancel Job
	if runRes.JobID != "" {
		w = httptest.NewRecorder()
		req, _ = http.NewRequest(http.MethodGet, "/v1/jobs/"+runRes.JobID, nil)
		server.Engine.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		w = httptest.NewRecorder()
		req, _ = http.NewRequest(http.MethodPost, "/v1/jobs/"+runRes.JobID+"/cancels", nil)
		server.Engine.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	}

	// Audit, MIB, and System
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/v1/mibs/trees", nil)
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/v1/audit/logs", nil)
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/v1/audit/exports", nil)
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/v1/system/backups", nil)
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/v1/system/restores", nil)
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Delete scenario
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodDelete, "/v1/scenarios/test-scenario", nil)
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGateway_ValidationAndErrorPaths(t *testing.T) {
	t.Parallel()
	server := setupTestServer(t, "gw_errors")

	// 404 Targets
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/v1/targets/nonexistent", nil)
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPut, "/v1/targets/nonexistent", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodDelete, "/v1/targets/nonexistent", nil)
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/v1/targets/nonexistent/drain", nil)
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// 404 Scenarios
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/v1/scenarios/nonexistent", nil)
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPut, "/v1/scenarios/nonexistent", bytes.NewReader([]byte(`{"dsl_yaml": "name: non"}`)))
	req.Header.Set("Content-Type", "application/json")
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodDelete, "/v1/scenarios/nonexistent", nil)
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/v1/scenarios/nonexistent/runs", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// 400 Bad Requests
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/v1/credentials", bytes.NewReader([]byte(`{"invalid": "json"`)))
	req.Header.Set("Content-Type", "application/json")
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/v1/targets", bytes.NewReader([]byte(`{"invalid": "json"`)))
	req.Header.Set("Content-Type", "application/json")
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/v1/scenarios", bytes.NewReader([]byte(`{"invalid": "json"`)))
	req.Header.Set("Content-Type", "application/json")
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
