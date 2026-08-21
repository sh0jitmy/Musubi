#!/usr/bin/env bash
set -euo pipefail

echo "===> [E2E] Starting Docker Compose E2E Test Suite..."

cleanup() {
    echo "===> [E2E] Tearing down Docker Compose stack..."
    docker compose down -v --remove-orphans > /dev/null 2>&1 || true
}
trap cleanup EXIT

# 1. Start Stack
docker compose up -d --build

# 2. Wait for Server Health
echo "===> [E2E] Waiting for Musubi Server..."
for i in {1..30}; do
    if curl -sf http://localhost:8080/v1/system/healthz > /dev/null 2>&1; then
        echo "Musubi Server is healthy!"
        break
    fi
    sleep 2
done

# 3. Test API Flow
echo "===> [E2E] 1. Testing Credential & Target Creation..."
curl -sf -X POST http://localhost:8080/v1/credentials \
  -H "Content-Type: application/json" \
  -d '{"name": "v3-e2e", "version": "v3", "sec_level": "authPriv", "username": "admin"}'

curl -sf -X POST http://localhost:8080/v1/targets \
  -H "Content-Type: application/json" \
  -d '{"name": "spine1", "host": "mock-snmp-agent", "port": 161, "credential_id": "cred-v3-e2e"}'

echo "===> [E2E] 2. Testing Target Ping..."
curl -sf -X POST http://localhost:8080/v1/targets/spine1/ping

echo "===> [E2E] 3. Testing Scenario Register and Execution..."
SCENARIO_JSON='{
  "name": "e2e-spine-check",
  "dsl_yaml": "name: e2e-spine-check\ntarget_locks: [spine1]\nsteps:\n  - id: s1\n    target: spine1\n"
}'
curl -sf -X POST http://localhost:8080/v1/scenarios \
  -H "Content-Type: application/json" \
  -d "$SCENARIO_JSON"

RUN_OUT=$(curl -sf -X POST http://localhost:8080/v1/scenarios/e2e-spine-check/runs -H "Content-Type: application/json" -d '{}')
echo "Run response: $RUN_OUT"

echo "===> [E2E] 4. Testing Target Drain Mode..."
curl -sf -X POST http://localhost:8080/v1/targets/spine1/drain

echo "===> [E2E] 5. Testing Grafana & VictoriaMetrics accessibility..."
curl -sf http://localhost:8428/health > /dev/null
curl -sf http://localhost:3000/api/health > /dev/null

echo "===> [E2E] 6. Testing Grafana UI Rendering, Metric Assertions & HTML Report Generation..."
python3 scripts/test_grafana_ui.py

echo "===> [E2E] ✅ ALL DOCKER E2E TESTS AND GRAFANA UI VERIFICATIONS PASSED SUCCESSFULLY!"
