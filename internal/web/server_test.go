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

package web_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sh0jitmy/musubi/internal/web"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func setupMockCoreServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	// Metrics endpoint
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		metricsOutput := `# HELP go_goroutines Number of goroutines that currently exist.
# TYPE go_goroutines gauge
go_goroutines 14
# HELP go_memstats_alloc_bytes Number of bytes allocated and still in use.
# TYPE go_memstats_alloc_bytes gauge
go_memstats_alloc_bytes 12582912
# HELP go_memstats_sys_bytes Number of bytes obtained from system.
# TYPE go_memstats_sys_bytes gauge
go_memstats_sys_bytes 26214400
# HELP process_resident_memory_bytes Resident memory size in bytes.
# TYPE process_resident_memory_bytes gauge
process_resident_memory_bytes 18874368
# HELP musubi_snmp_traffic_bytes_total Total SNMP traffic in bytes
# TYPE musubi_snmp_traffic_bytes_total counter
musubi_snmp_traffic_bytes_total{direction="inbound",target="spine1"} 40960
`
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write([]byte(metricsOutput))
	})

	// Targets endpoint
	mux.HandleFunc("/v1/targets", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{
					"name":          "spine1",
					"host":          "127.0.0.1",
					"port":          161,
					"status":        "ONLINE",
					"credential_id": "v2c-default",
					"updated_at":    time.Now().UTC().Format(time.RFC3339),
				},
			},
			"total": 1,
		})
	})

	// Target Ping & Drain
	mux.HandleFunc("/v1/targets/spine1/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"PONG"}`))
	})
	mux.HandleFunc("/v1/targets/spine1/drain", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"DRAINING"}`))
	})

	// State Transitions
	mux.HandleFunc("/v1/states/transitions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{
					"target":     "spine1",
					"state_key":  ".1.3.6.1.2.1.1.1.0",
					"old_value":  "",
					"new_value":  "Musubi Linux Mock",
					"trigger":    "BULKGET",
					"created_at": time.Now().UTC().Format(time.RFC3339),
				},
			},
			"total": 1,
		})
	})

	// Jobs
	mux.HandleFunc("/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{
					"id":               "job-001",
					"scenario_id":      "spine1-check",
					"scenario_version": 1,
					"status":           "SUCCESS",
					"triggered_by":     "admin",
					"created_at":       time.Now().UTC().Format(time.RFC3339),
				},
			},
			"total": 1,
		})
	})

	mux.HandleFunc("/v1/jobs/job-001", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":               "job-001",
			"scenario_id":      "spine1-check",
			"scenario_version": 1,
			"status":           "SUCCESS",
			"triggered_by":     "admin",
		})
	})

	// Scenarios
	mux.HandleFunc("/v1/scenarios", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"spine1-check","name":"spine1-check"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{
					"name":            "spine1-check",
					"description":     "Test description",
					"current_version": 1,
					"created_at":      time.Now().UTC().Format(time.RFC3339),
				},
			},
			"total": 1,
		})
	})

	mux.HandleFunc("/v1/scenarios/spine1-check/runs", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"job_id":"job-001","status":"RUNNING"}`))
	})

	// Audit Logs
	mux.HandleFunc("/v1/audit/logs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{
					"id":         "audit-1",
					"user_id":    "admin",
					"role":       "Administrator",
					"action":     "TARGET_PING",
					"target_id":  "spine1",
					"ip":         "127.0.0.1",
					"created_at": time.Now().UTC().Format(time.RFC3339),
				},
			},
			"total": 1,
		})
	})

	return httptest.NewServer(mux)
}

func TestWeb_PagesAndRouting(t *testing.T) {
	t.Parallel()

	mockCore := setupMockCoreServer(t)
	t.Cleanup(mockCore.Close)

	client := web.NewClient(mockCore.URL, 2*time.Second)
	server, err := web.NewServer(client, "3001")
	require.NoError(t, err)

	testCases := []struct {
		name            string
		path            string
		method          string
		expectedStatus  int
		containsBody    string
		notContainsBody string
	}{
		{
			name:           "Health Check",
			path:           "/healthz",
			method:         http.MethodGet,
			expectedStatus: http.StatusOK,
			containsBody:   "OK",
		},
		{
			name:            "Dashboard Overview Page",
			path:            "/",
			method:          http.MethodGet,
			expectedStatus:  http.StatusOK,
			containsBody:    "統合監視ダッシュボード",
			notContainsBody: "シナリオ新規作成",
		},
		{
			name:            "Scenario Studio Page",
			path:            "/scenarios",
			method:          http.MethodGet,
			expectedStatus:  http.StatusOK,
			containsBody:    "シナリオスタジオ",
			notContainsBody: "統合監視ダッシュボード",
		},
		{
			name:           "Static CSS Asset",
			path:           "/static/css/dashboard.css",
			method:         http.MethodGet,
			expectedStatus: http.StatusOK,
			containsBody:   "--bg-base",
		},
		{
			name:           "Static HTMX JS Asset",
			path:           "/static/js/htmx.min.js",
			method:         http.MethodGet,
			expectedStatus: http.StatusOK,
			containsBody:   "htmx",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()
			server.Engine.ServeHTTP(w, req)

			assert.Equal(t, tc.expectedStatus, w.Code)
			if tc.containsBody != "" {
				assert.Contains(t, w.Body.String(), tc.containsBody)
			}
			if tc.notContainsBody != "" {
				assert.NotContains(t, w.Body.String(), tc.notContainsBody)
			}
		})
	}
}

func TestWeb_HTMXComponents(t *testing.T) {
	t.Parallel()

	mockCore := setupMockCoreServer(t)
	t.Cleanup(mockCore.Close)

	client := web.NewClient(mockCore.URL, 2*time.Second)
	server, err := web.NewServer(client, "3001")
	require.NoError(t, err)

	components := []struct {
		name         string
		path         string
		expectedWord string
	}{
		{
			name:         "System Metrics Component",
			path:         "/ui/components/system-metrics",
			expectedWord: "Process CPU Usage",
		},
		{
			name:         "Targets Table Component",
			path:         "/ui/components/targets-table",
			expectedWord: "spine1",
		},
		{
			name:         "MIBs Table Component",
			path:         "/ui/components/mibs-table",
			expectedWord: ".1.3.6.1.2.1.1.1.0",
		},
		{
			name:         "Jobs Table Component",
			path:         "/ui/components/jobs-table",
			expectedWord: "job-001",
		},
		{
			name:         "Audit Logs Component",
			path:         "/ui/components/audit-logs",
			expectedWord: "TARGET_PING",
		},
		{
			name:         "Scenarios List Component",
			path:         "/ui/components/scenarios-list",
			expectedWord: "spine1-check",
		},
	}

	for _, c := range components {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, c.path, nil)
			w := httptest.NewRecorder()
			server.Engine.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.Contains(t, w.Body.String(), c.expectedWord)
		})
	}
}

func TestWeb_ScenarioSubmissionAndExecution(t *testing.T) {
	t.Parallel()

	mockCore := setupMockCoreServer(t)
	t.Cleanup(mockCore.Close)

	client := web.NewClient(mockCore.URL, 2*time.Second)
	server, err := web.NewServer(client, "3001")
	require.NoError(t, err)

	t.Run("Create Scenario - Success", func(t *testing.T) {
		t.Parallel()
		formData := url.Values{
			"name":        {"test-scenario-new"},
			"description": {"New test scenario"},
			"dsl_yaml":    {"name: test-scenario-new\ntarget_locks: [spine1]\nsteps: []\n"},
		}
		req := httptest.NewRequest(http.MethodPost, "/ui/scenarios", strings.NewReader(formData.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		server.Engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "正常に登録されました")
	})

	t.Run("Create Scenario - Missing Params", func(t *testing.T) {
		t.Parallel()
		formData := url.Values{
			"name": {""},
		}
		req := httptest.NewRequest(http.MethodPost, "/ui/scenarios", strings.NewReader(formData.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		server.Engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "必須項目です")
	})

	t.Run("Run Scenario - Success", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodPost, "/ui/scenarios/spine1-check/run", nil)
		w := httptest.NewRecorder()
		server.Engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "job-001")
	})

	t.Run("Job Status Polling", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/ui/jobs/job-001/status", nil)
		w := httptest.NewRecorder()
		server.Engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "COMPLETED (SUCCESS)")
	})
}

func TestWeb_TargetActions(t *testing.T) {
	t.Parallel()

	mockCore := setupMockCoreServer(t)
	t.Cleanup(mockCore.Close)

	client := web.NewClient(mockCore.URL, 2*time.Second)
	server, err := web.NewServer(client, "3001")
	require.NoError(t, err)

	t.Run("Target Ping", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodPost, "/ui/targets/spine1/ping", nil)
		w := httptest.NewRecorder()
		server.Engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "spine1")
	})

	t.Run("Target Drain", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodPost, "/ui/targets/spine1/drain", nil)
		w := httptest.NewRecorder()
		server.Engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "DRAINING")
	})
}

func TestWeb_SSGExport(t *testing.T) {
	t.Parallel()

	mockCore := setupMockCoreServer(t)
	t.Cleanup(mockCore.Close)

	client := web.NewClient(mockCore.URL, 2*time.Second)
	tempDir, err := os.MkdirTemp("", "musubi-ssg-test-*")
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.RemoveAll(tempDir)
	})

	err = web.ExportStaticSite(tempDir, client)
	require.NoError(t, err)

	// Check output files
	indexPath := filepath.Join(tempDir, "index.html")
	scenariosPath := filepath.Join(tempDir, "scenarios.html")
	cssPath := filepath.Join(tempDir, "static", "css", "dashboard.css")
	jsPath := filepath.Join(tempDir, "static", "js", "htmx.min.js")

	assert.FileExists(t, indexPath)
	assert.FileExists(t, scenariosPath)
	assert.FileExists(t, cssPath)
	assert.FileExists(t, jsPath)

	//nolint:gosec // test artifact file read
	indexContent, err := os.ReadFile(indexPath)
	require.NoError(t, err)
	assert.Contains(t, string(indexContent), "統合ダッシュボード")

	//nolint:gosec // test artifact file read
	scenariosContent, err := os.ReadFile(scenariosPath)
	require.NoError(t, err)
	assert.Contains(t, string(scenariosContent), "シナリオスタジオ")
}

func TestClient_EdgeCases(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("Invalid Host / Network Errors", func(t *testing.T) {
		t.Parallel()
		client := web.NewClient("http://127.0.0.1:59999", 500*time.Millisecond)

		_, err := client.GetSystemMetrics(ctx)
		require.Error(t, err)

		_, err = client.ListTargets(ctx)
		require.Error(t, err)

		_, err = client.ListStateTransitions(ctx)
		require.Error(t, err)

		_, err = client.ListJobs(ctx)
		require.Error(t, err)

		_, err = client.GetJob(ctx, "nonexistent")
		require.Error(t, err)

		_, err = client.ListScenarios(ctx)
		require.Error(t, err)

		err = client.CreateScenario(ctx, "foo", "bar", "yaml")
		require.Error(t, err)

		_, err = client.RunScenario(ctx, "foo")
		require.Error(t, err)

		err = client.PingTarget(ctx, "foo")
		require.Error(t, err)

		err = client.DrainTarget(ctx, "foo")
		require.Error(t, err)

		_, err = client.ListAuditLogs(ctx)
		require.Error(t, err)
	})
}

func TestWeb_HandlerFallbacksAndErrors(t *testing.T) {
	t.Parallel()

	// Server pointing to a closed port to trigger client errors and fallback branches
	badClient := web.NewClient("http://127.0.0.1:59998", 200*time.Millisecond)
	server, err := web.NewServer(badClient, "3002")
	require.NoError(t, err)

	t.Run("Dashboard Page with Core Down", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		server.Engine.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "Musubi Dashboard")
	})

	t.Run("Scenarios Page with Core Down", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/scenarios", nil)
		w := httptest.NewRecorder()
		server.Engine.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "シナリオスタジオ")
	})

	t.Run("Components with Core Down", func(t *testing.T) {
		t.Parallel()
		paths := []string{
			"/ui/components/system-metrics",
			"/ui/components/targets-table",
			"/ui/components/mibs-table",
			"/ui/components/jobs-table",
			"/ui/components/audit-logs",
			"/ui/components/scenarios-list",
		}
		for _, p := range paths {
			req := httptest.NewRequest(http.MethodGet, p, nil)
			w := httptest.NewRecorder()
			server.Engine.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
		}
	})

	t.Run("Create Scenario Failure", func(t *testing.T) {
		t.Parallel()
		formData := url.Values{
			"name":     {"fail-scenario"},
			"dsl_yaml": {"name: fail-scenario\n"},
		}
		req := httptest.NewRequest(http.MethodPost, "/ui/scenarios", strings.NewReader(formData.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		server.Engine.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "登録失敗")
	})

	t.Run("Run Scenario Failure", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodPost, "/ui/scenarios/nonexistent/run", nil)
		w := httptest.NewRecorder()
		server.Engine.ServeHTTP(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "実行エラー")
	})

	t.Run("Job Status Not Found", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/ui/jobs/nonexistent/status", nil)
		w := httptest.NewRecorder()
		server.Engine.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "Job Not Found")
	})

	t.Run("Target Ping Fallback", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodPost, "/ui/targets/unregistered-target/ping", nil)
		w := httptest.NewRecorder()
		server.Engine.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "unregistered-target")
	})

	t.Run("Target Drain Fallback", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodPost, "/ui/targets/unregistered-target/drain", nil)
		w := httptest.NewRecorder()
		server.Engine.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "DRAINING")
	})

	t.Run("SSG Export with Nil Client", func(t *testing.T) {
		t.Parallel()
		tempDir, err := os.MkdirTemp("", "musubi-ssg-nil-*")
		require.NoError(t, err)
		t.Cleanup(func() {
			_ = os.RemoveAll(tempDir)
		})

		err = web.ExportStaticSite(tempDir, nil)
		require.NoError(t, err)
		assert.FileExists(t, filepath.Join(tempDir, "index.html"))
	})
}
