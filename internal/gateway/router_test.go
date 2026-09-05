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
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	_ "github.com/glebarez/go-sqlite"
	"github.com/gosnmp/gosnmp"
	"github.com/sh0jitmy/musubi/internal/collector"
	"github.com/sh0jitmy/musubi/internal/common/notification"
	"github.com/sh0jitmy/musubi/internal/database"
	"github.com/sh0jitmy/musubi/internal/gateway"
	"github.com/sh0jitmy/musubi/internal/state"
	"github.com/sh0jitmy/musubi/internal/testutil/pcap"
	"github.com/sh0jitmy/musubi/internal/testutil/snmpmock"
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

func TestGateway_SystemPurgeAndEvents(t *testing.T) {
	t.Parallel()
	server := setupTestServer(t, "gw_purge_events")

	// 1. Test POST /v1/system/purge
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/v1/system/purge", bytes.NewReader([]byte(`{"days": 30}`)))
	req.Header.Set("Content-Type", "application/json")
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// 2. Test GET /v1/events/poll
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/v1/events/poll?since=0&timeout=50ms", nil)
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// 3. Test GET /v1/events/stream with context cancel
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	w = httptest.NewRecorder()
	req, _ = http.NewRequestWithContext(ctx, http.MethodGet, "/v1/events/stream", nil)
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// 4. Test TargetProviderAdapter
	// Create credential profile & target
	w = httptest.NewRecorder()
	//nolint:gosec // mock test credentials
	credBody := `{"name": "test-cred-v2", "version": "v2c", "community": "public"}`
	req, _ = http.NewRequest(http.MethodPost, "/v1/credentials", bytes.NewReader([]byte(credBody)))
	req.Header.Set("Content-Type", "application/json")
	server.Engine.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	var credResp struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &credResp)

	w = httptest.NewRecorder()
	targetBody := fmt.Sprintf(`{"name": "test-gw-target", "host": "127.0.0.1", "port": 161, "credential_id": "%s"}`, credResp.ID)
	req, _ = http.NewRequest(http.MethodPost, "/v1/targets", bytes.NewReader([]byte(targetBody)))
	req.Header.Set("Content-Type", "application/json")
	server.Engine.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	adapter := &gateway.TargetProviderAdapter{Client: server.EntClient}
	targetInfo, err := adapter.GetTarget(context.Background(), "test-gw-target")
	require.NoError(t, err)
	assert.Equal(t, "test-gw-target", targetInfo.Name)

	snmpCli, err := adapter.GetSNMPClient(context.Background(), "test-gw-target")
	require.NoError(t, err)
	assert.NotNil(t, snmpCli)
}

func TestGateway_AdhocScenario(t *testing.T) {
	t.Parallel()
	server := setupTestServer(t, "gw_adhoc")

	// Start local mock SNMP agent to serve requests and allow clean goroutine termination
	agent := snmpmock.NewMockAgent(nil)
	agentAddr, err := agent.Start()
	require.NoError(t, err)
	defer agent.Stop()

	_, portStr, err := net.SplitHostPort(agentAddr)
	require.NoError(t, err)
	agentPort, _ := strconv.Atoi(portStr)

	// 1. Setup credential and target
	//nolint:gosec // mock test credentials
	credBody := `{"name": "adhoc-cred", "version": "v2c", "community": "public"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/v1/credentials", bytes.NewReader([]byte(credBody)))
	req.Header.Set("Content-Type", "application/json")
	server.Engine.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	w = httptest.NewRecorder()
	targetBody := fmt.Sprintf(`{"name": "adhoc-target", "host": "127.0.0.1", "port": %d, "credential_id": "cred-adhoc-cred"}`, agentPort)
	req, _ = http.NewRequest(http.MethodPost, "/v1/targets", bytes.NewReader([]byte(targetBody)))
	req.Header.Set("Content-Type", "application/json")
	server.Engine.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	adhocDSL := `
name: adhoc-test-flow
target_locks: [adhoc-target]
steps:
  - id: step-1
    target: adhoc-target
    action: action.snmp_get
    params:
      oid: ".1.3.6.1.2.1.1.1.0"
  - id: step-2
    target: adhoc-target
    action: action.snmp_get
    params:
      oid: ".1.3.6.1.2.1.1.5.0"
`

	// 2. Test Synchronous Ad-hoc Execution (wait=true)
	syncPayload := map[string]any{
		"name":     "adhoc-sync-verification",
		"dsl_yaml": adhocDSL,
		"wait":     true,
	}
	syncBody, _ := json.Marshal(syncPayload)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/v1/scenarios/adhoc", bytes.NewReader(syncBody))
	req.Header.Set("Content-Type", "application/json")
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var syncResp map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &syncResp)
	require.NoError(t, err)
	assert.NotEmpty(t, syncResp["job_id"])
	assert.Equal(t, "adhoc-sync-verification", syncResp["scenario_id"])
	assert.Equal(t, "SUCCESS", syncResp["status"])

	// Verify that Job and AuditLog were persisted
	jobs, err := server.EntClient.Job.Query().All(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, jobs)

	audits, err := server.EntClient.AuditLog.Query().All(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, audits)

	// Verify that Scenario catalog (/v1/scenarios) was NOT contaminated
	scenarios, err := server.EntClient.Scenario.Query().All(context.Background())
	require.NoError(t, err)
	assert.Empty(t, scenarios, "Scenario catalog must remain clean without ephemeral ad-hoc entries")

	// 3. Test Asynchronous Ad-hoc Execution (wait=false)
	asyncPayload := map[string]any{
		"name":     "adhoc-async-run",
		"dsl_yaml": adhocDSL,
		"wait":     false,
	}
	asyncBody, _ := json.Marshal(asyncPayload)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/v1/scenarios/adhoc", bytes.NewReader(asyncBody))
	req.Header.Set("Content-Type", "application/json")
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusAccepted, w.Code)

	var asyncResp struct {
		JobID string `json:"job_id"`
	}
	err = json.Unmarshal(w.Body.Bytes(), &asyncResp)
	require.NoError(t, err)
	assert.NotEmpty(t, asyncResp.JobID)

	// Wait briefly for async job to finish cleanly
	time.Sleep(100 * time.Millisecond)

	// 4. Test Invalid YAML
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/v1/scenarios/adhoc", bytes.NewReader([]byte(`{"dsl_yaml": "invalid: [yaml: "}`)))
	req.Header.Set("Content-Type", "application/json")
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 5. Test Target Not Found
	nonExistentDSL := `
name: missing-target-flow
target_locks: [non-existent-device]
steps:
  - id: step-1
    target: non-existent-device
`
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/v1/scenarios/adhoc", bytes.NewReader([]byte(fmt.Sprintf(`{"dsl_yaml": %q}`, nonExistentDSL))))
	req.Header.Set("Content-Type", "application/json")
	server.Engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestGateway_TraditionalRegisterAndRun_FullSuccess(t *testing.T) {
	t.Parallel()
	server := setupTestServer(t, "gw_trad_success")

	// 1. Setup PCAP Recorder for packet verification
	pcapPath := filepath.Join("../../test_reports", "traditional_e2e_flow.pcap")
	recorder, err := pcap.NewPcapRecorder(pcapPath)
	require.NoError(t, err)
	defer recorder.Close()

	// 2. Start local Trap & Inform listener on dynamic free port
	listener := collector.NewListener("127.0.0.1:0", func(target string, oid string, value any, trigger string) {
		server.StateRepo.SetRaw("trad-target", oid, value, trigger)
	})
	err = listener.Start()
	require.NoError(t, err)
	defer listener.Stop()

	trapAddr := listener.Addr()
	_, trapPortStr, err := net.SplitHostPort(trapAddr)
	require.NoError(t, err)
	trapPort, _ := strconv.Atoi(trapPortStr)

	// 3. Start local mock agent and hook PCAP packet recorder
	agent := snmpmock.NewMockAgent(nil)
	agent.SetTrapTarget(trapAddr)
	agent.SetPacketHook(recorder.RecordUDPPacket)

	agentAddr, err := agent.Start()
	require.NoError(t, err)
	defer agent.Stop()

	_, portStr, err := net.SplitHostPort(agentAddr)
	require.NoError(t, err)
	agentPort, _ := strconv.Atoi(portStr)

	// 4. Setup credential and target
	//nolint:gosec // mock test credentials
	credBody := `{"name": "trad-cred", "version": "v2c", "community": "public"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/v1/credentials", bytes.NewReader([]byte(credBody)))
	req.Header.Set("Content-Type", "application/json")
	server.Engine.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	w = httptest.NewRecorder()
	targetBody := fmt.Sprintf(`{"name": "trad-target", "host": "127.0.0.1", "port": %d, "credential_id": "cred-trad-cred"}`, agentPort)
	req, _ = http.NewRequest(http.MethodPost, "/v1/targets", bytes.NewReader([]byte(targetBody)))
	req.Header.Set("Content-Type", "application/json")
	server.Engine.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	// 5. Register scenario to scenario catalog via POST /v1/scenarios
	tradDSL := `
name: trad-scenario-01
target_locks: [trad-target]
steps:
  - id: s1_get_descr
    target: trad-target
    action: action.snmp_get
    params:
      oid: ".1.3.6.1.2.1.1.1.0"
  - id: s2_set_admin_down
    target: trad-target
    action: action.snmp_set
    params:
      oid: ".1.3.6.1.2.1.2.2.1.7.1"
      type: "int"
      value: 2
  - id: s3_wait_oper_down
    target: trad-target
    wait:
      until: "raw['trad-target']['.1.3.6.1.2.1.2.2.1.8.1'] == 2 || raw['trad-target']['1.3.6.1.2.1.2.2.1.8.1'] == 2"
      timeout: "5s"
      interval: "50ms"
  - id: s4_set_admin_up
    target: trad-target
    action: action.snmp_set
    params:
      oid: ".1.3.6.1.2.1.2.2.1.7.1"
      type: "int"
      value: 1
  - id: s5_wait_oper_up
    target: trad-target
    wait:
      until: "raw['trad-target']['.1.3.6.1.2.1.2.2.1.8.1'] == 1 || raw['trad-target']['1.3.6.1.2.1.2.2.1.8.1'] == 1"
      timeout: "5s"
      interval: "50ms"
  - id: s6_get_sysname
    target: trad-target
    action: action.snmp_get
    params:
      oid: ".1.3.6.1.2.1.1.5.0"
`
	scPayload := map[string]any{
		"name":        "trad-scenario-01",
		"description": "Traditional catalog scenario with PCAP verification",
		"dsl_yaml":    tradDSL,
	}
	scBody, _ := json.Marshal(scPayload)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/v1/scenarios", bytes.NewReader(scBody))
	req.Header.Set("Content-Type", "application/json")
	server.Engine.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	// 6. Verify scenario is listed in catalog
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/v1/scenarios", nil)
	server.Engine.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "trad-scenario-01")

	// 7. Get scenario details via GET /v1/scenarios/{id}
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/v1/scenarios/trad-scenario-01", nil)
	server.Engine.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "trad-scenario-01")

	// 8. Execute scenario via POST /v1/scenarios/{id}/runs
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/v1/scenarios/trad-scenario-01/runs", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	server.Engine.ServeHTTP(w, req)
	require.Equal(t, http.StatusAccepted, w.Code)

	var runResp struct {
		JobID string `json:"job_id"`
	}
	err = json.Unmarshal(w.Body.Bytes(), &runResp)
	require.NoError(t, err)
	assert.NotEmpty(t, runResp.JobID)

	// 9. Poll GET /v1/jobs/{id} until status becomes SUCCESS
	var finalStatus string
	for i := 0; i < 60; i++ {
		time.Sleep(50 * time.Millisecond)
		w = httptest.NewRecorder()
		req, _ = http.NewRequest(http.MethodGet, "/v1/jobs/"+runResp.JobID, nil)
		server.Engine.ServeHTTP(w, req)
		if w.Code == http.StatusOK {
			var jobResp struct {
				Status string `json:"status"`
			}
			if uErr := json.Unmarshal(w.Body.Bytes(), &jobResp); uErr == nil {
				finalStatus = jobResp.Status
				if finalStatus == "SUCCESS" {
					break
				}
			}
		}
	}
	require.Equal(t, "SUCCESS", finalStatus, "Traditional scenario execution must successfully transition to SUCCESS")

	// 10. Close recorder and verify PCAP packets on disk
	recorder.Close()

	frames, err := pcap.ReadFrames(pcapPath)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(frames), 10, "PCAP must contain at least 10 packets for 6-step scenario")

	// Verify exact scenario packet flow in PCAP:
	// - GetRequest (.1.3.6.1.2.1.1.1.0) & GetResponse
	// - SetRequest (.1.3.6.1.2.1.2.2.1.7.1 = 2) & GetResponse
	// - InformRequest (.1.3.6.1.2.1.2.2.1.8.1 = 2) & GetResponse (ACK)
	// - SetRequest (.1.3.6.1.2.1.2.2.1.7.1 = 1) & GetResponse
	// - InformRequest (.1.3.6.1.2.1.2.2.1.8.1 = 1) & GetResponse (ACK)
	// - GetRequest (.1.3.6.1.2.1.1.5.0) & GetResponse
	var hasGetDescr, hasSetDown, hasInformDown, hasSetUp, hasInformUp, hasGetName bool
	var getResponses, ackResponses int

	for _, f := range frames {
		if f.SNMPPacket == nil {
			continue
		}
		switch f.SNMPPacket.PDUType {
		case gosnmp.GetRequest:
			for _, v := range f.SNMPPacket.Variables {
				if v.Name == ".1.3.6.1.2.1.1.1.0" {
					hasGetDescr = true
				}
				if v.Name == ".1.3.6.1.2.1.1.5.0" {
					hasGetName = true
				}
			}
		case gosnmp.SetRequest:
			for _, v := range f.SNMPPacket.Variables {
				if v.Name == ".1.3.6.1.2.1.2.2.1.7.1" {
					if v.Value == 2 || v.Value == int64(2) {
						hasSetDown = true
					}
					if v.Value == 1 || v.Value == int64(1) {
						hasSetUp = true
					}
				}
			}
		case gosnmp.InformRequest:
			for _, v := range f.SNMPPacket.Variables {
				if v.Name == ".1.3.6.1.2.1.2.2.1.8.1" {
					if v.Value == 2 || v.Value == int64(2) {
						hasInformDown = true
					}
					if v.Value == 1 || v.Value == int64(1) {
						hasInformUp = true
					}
				}
			}
		case gosnmp.GetResponse:
			if f.DstPort == trapPort || f.SrcPort == trapPort {
				ackResponses++
			} else {
				getResponses++
			}
		}
	}

	assert.True(t, hasGetDescr, "PCAP must record Step 1 GetRequest for sysDescr (.1.3.6.1.2.1.1.1.0)")
	assert.True(t, hasSetDown, "PCAP must record Step 2 SetRequest for ifAdminStatus=2 (.1.3.6.1.2.1.2.2.1.7.1)")
	assert.True(t, hasInformDown, "PCAP must record Step 3 InformRequest for ifOperStatus=2 (.1.3.6.1.2.1.2.2.1.8.1)")
	assert.True(t, hasSetUp, "PCAP must record Step 4 SetRequest for ifAdminStatus=1 (.1.3.6.1.2.1.2.2.1.7.1)")
	assert.True(t, hasInformUp, "PCAP must record Step 5 InformRequest for ifOperStatus=1 (.1.3.6.1.2.1.2.2.1.8.1)")
	assert.True(t, hasGetName, "PCAP must record Step 6 GetRequest for sysName (.1.3.6.1.2.1.1.5.0)")
	assert.GreaterOrEqual(t, getResponses, 4, "PCAP must record Get/Set responses")
	assert.GreaterOrEqual(t, ackResponses, 2, "PCAP must record Inform ACK responses")
}
