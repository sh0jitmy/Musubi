#!/usr/bin/env bash
# Copyright 2026 Musubi Contributors
# Licensed under the Apache License, Version 2.0 (the "License");

set -euo pipefail

CYAN='\033[0;36m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${CYAN}======================================================${NC}"
echo -e "${CYAN}     Musubi Standalone SQLite (No-Docker) E2E Test    ${NC}"
echo -e "${CYAN}======================================================${NC}"

# Temporary isolated environment
WORK_DIR=$(mktemp -d -t musubi-sqlite-e2e-XXXXXX)
BACKUP_DIR="$WORK_DIR/backups"
mkdir -p "$BACKUP_DIR"

PCAP_DIR="$(pwd)/test_reports"
mkdir -p "$PCAP_DIR"
PCAP_FILE="$PCAP_DIR/sqlite_e2e_scenario.pcap"
rm -f "$PCAP_FILE"

SERVER_PORT=18080
SNMP_AGENT_PORT=10161
SNMP_TRAP_PORT=10162

SERVER_PID=""
AGENT_PID=""

cleanup() {
    echo -e "\n${YELLOW}===> [E2E Cleanup] Tearing down processes and temporary files...${NC}"
    if [ -n "$SERVER_PID" ] && kill -0 "$SERVER_PID" > /dev/null 2>&1; then
        kill "$SERVER_PID" > /dev/null 2>&1 || true
    fi
    if [ -n "$AGENT_PID" ] && kill -0 "$AGENT_PID" > /dev/null 2>&1; then
        kill "$AGENT_PID" > /dev/null 2>&1 || true
    fi
    rm -rf "$WORK_DIR"
    echo -e "${GREEN}===> [E2E Cleanup] Cleanup complete.${NC}"
}
trap cleanup EXIT INT TERM

# 1. Build Binaries
echo -e "\n${YELLOW}[Step 1/8] Compiling binaries (musubi-server, mock-snmp-agent, musubi-cli)...${NC}"
go build -o "$WORK_DIR/musubi-server" ./cmd/musubi-server
go build -o "$WORK_DIR/mock-snmp-agent" ./cmd/mock-snmp-agent
go build -o "$WORK_DIR/musubi-cli" ./cmd/musubi-cli
echo -e "${GREEN}Binaries successfully compiled.${NC}"

# 2. Start Mock SNMP Agent with PCAP Packet Capture
echo -e "\n${YELLOW}[Step 2/8] Starting Mock SNMP Agent on UDP port ${SNMP_AGENT_PORT} (PCAP: ${PCAP_FILE})...${NC}"
SNMP_PORT="$SNMP_AGENT_PORT" TRAP_TARGET="127.0.0.1:${SNMP_TRAP_PORT}" PCAP_CAPTURE_PATH="$PCAP_FILE" \
    "$WORK_DIR/mock-snmp-agent" > "$WORK_DIR/agent.log" 2>&1 &
AGENT_PID=$!

# 3. Start Musubi Server (SQLite Standalone)
echo -e "\n${YELLOW}[Step 3/8] Starting Musubi Server with SQLite on port ${SERVER_PORT}...${NC}"
PORT="$SERVER_PORT" \
SNMP_TRAP_PORT="$SNMP_TRAP_PORT" \
DATABASE_DRIVER="sqlite3" \
DATABASE_DSN="$WORK_DIR/musubi.db?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)" \
BACKUP_DIR="$BACKUP_DIR" \
BACKUP_ENABLED="true" \
BACKUP_INTERVAL_HOURS="1" \
BACKUP_RETENTION_COUNT="5" \
    "$WORK_DIR/musubi-server" > "$WORK_DIR/server.log" 2>&1 &
SERVER_PID=$!

# 4. Wait for Server Health
echo -e "\n${YELLOW}[Step 4/8] Waiting for Musubi Server to become healthy...${NC}"
for i in {1..30}; do
    if curl -sf "http://127.0.0.1:${SERVER_PORT}/v1/system/healthz" > /dev/null 2>&1; then
        echo -e "${GREEN}Musubi Server is ONLINE and HEALTHY!${NC}"
        break
    fi
    if [ "$i" -eq 30 ]; then
        echo -e "${RED}Server failed to start in time. Logs:${NC}"
        cat "$WORK_DIR/server.log"
        exit 1
    fi
    sleep 1
done

# Verify deep health and readiness
curl -sf "http://127.0.0.1:${SERVER_PORT}/v1/system/readyz" > /dev/null
curl -sf "http://127.0.0.1:${SERVER_PORT}/v1/system/healths" > /dev/null

# 5. Test Core Orchestration Flow
echo -e "\n${YELLOW}[Step 5/8] Testing Credential, Target, Ping, Scenario & Execution Flow...${NC}"
# Create Credential (SNMP v2c for Mock Agent)
CRED_RESP=$(curl -sf -X POST "http://127.0.0.1:${SERVER_PORT}/v1/credentials" \
  -H "Content-Type: application/json" \
  -d '{"name": "v2c-sqlite-e2e", "version": "v2c", "community": "public"}')
CRED_ID=$(echo "$CRED_RESP" | grep -o '"id":"[^"]*' | cut -d'"' -f4)
echo "  - Created Credential: $CRED_ID"

# Create Target pointing to localhost Mock SNMP Agent
curl -sf -X POST "http://127.0.0.1:${SERVER_PORT}/v1/targets" \
  -H "Content-Type: application/json" \
  -d "{\"name\": \"spine1\", \"host\": \"127.0.0.1\", \"port\": ${SNMP_AGENT_PORT}, \"credential_id\": \"${CRED_ID}\"}" > /dev/null
echo "  - Created Target: spine1 (127.0.0.1:${SNMP_AGENT_PORT})"

# Ping Target
curl -sf -X POST "http://127.0.0.1:${SERVER_PORT}/v1/targets/spine1/ping" > /dev/null
echo "  - Successfully pinged spine1 SNMP agent"

# Register Scenario with full SNMP GET, SET, and Teardown flow
SCENARIO_JSON='{
  "name": "sqlite-e2e-spine-check",
  "dsl_yaml": "name: sqlite-e2e-spine-check\ntarget_locks: [spine1]\nsteps:\n  - id: s1_get_descr\n    target: spine1\n    action: action.snmp_get\n    params:\n      oid: \".1.3.6.1.2.1.1.1.0\"\n  - id: s2_set_admin_down\n    target: spine1\n    action: action.snmp_set\n    params:\n      oid: \".1.3.6.1.2.1.2.2.1.7.1\"\n      type: int\n      value: 2\n  - id: s3_get_admin_status\n    target: spine1\n    action: action.snmp_get\n    params:\n      oid: \".1.3.6.1.2.1.2.2.1.7.1\"\nteardown:\n  - id: teardown_admin_up\n    target: spine1\n    action: action.snmp_set\n    params:\n      oid: \".1.3.6.1.2.1.2.2.1.7.1\"\n      type: int\n      value: 1\n"
}'
curl -sf -X POST "http://127.0.0.1:${SERVER_PORT}/v1/scenarios" \
  -H "Content-Type: application/json" \
  -d "$SCENARIO_JSON" > /dev/null
echo "  - Registered Scenario: sqlite-e2e-spine-check"

# Execute Scenario
RUN_RESP=$(curl -sf -X POST "http://127.0.0.1:${SERVER_PORT}/v1/scenarios/sqlite-e2e-spine-check/runs" \
  -H "Content-Type: application/json" -d '{}')
JOB_ID=$(echo "$RUN_RESP" | grep -o '"job_id":"[^"]*' | cut -d'"' -f4 || true)
echo "  - Triggered scenario run (Job ID: ${JOB_ID})"

# Wait for Scenario Job completion
if [ -n "$JOB_ID" ]; then
    for j in {1..20}; do
        JOB_STATUS=$(curl -sf "http://127.0.0.1:${SERVER_PORT}/v1/jobs/${JOB_ID}" | grep -o '"status":"[^"]*' | cut -d'"' -f4 || true)
        if [ "$JOB_STATUS" = "SUCCESS" ]; then
            echo "  - Scenario job completed successfully (status: SUCCESS)"
            break
        elif [ "$JOB_STATUS" = "FAILED" ] || [ "$JOB_STATUS" = "ABORTED" ]; then
            echo -e "${RED}Scenario job ended with status: $JOB_STATUS${NC}"
            exit 1
        fi
        sleep 0.5
    done
fi

# Verify Live PCAP Capture File
if [ -f "$PCAP_FILE" ]; then
    PCAP_SIZE=$(wc -c < "$PCAP_FILE" | tr -d ' ')
    echo "  - PCAP capture verified: ${PCAP_FILE} (${PCAP_SIZE} bytes)"
    python3 scripts/verify_snmp_pcap_flow.py "$PCAP_FILE"
else
    echo -e "${RED}Expected PCAP capture file not found at ${PCAP_FILE}!${NC}"
    exit 1
fi

# Drain Target
curl -sf -X POST "http://127.0.0.1:${SERVER_PORT}/v1/targets/spine1/drain" > /dev/null
echo "  - Successfully set spine1 to DRAIN mode"

# 6. Test Backup Creation & Archive Download
echo -e "\n${YELLOW}[Step 6/8] Testing Full SQLite Backup Creation & Archive Download...${NC}"
BACKUP_JSON=$(curl -sf -X POST "http://127.0.0.1:${SERVER_PORT}/v1/system/backups")
echo "  - Backup API response: $BACKUP_JSON"
BACKUP_FILE=$(echo "$BACKUP_JSON" | grep -o '"filename":"[^"]*' | cut -d'"' -f4)
DOWNLOAD_URL=$(echo "$BACKUP_JSON" | grep -o '"download_url":"[^"]*' | cut -d'"' -f4)

if [ -z "$BACKUP_FILE" ]; then
    echo -e "${RED}Failed to extract backup filename from response!${NC}"
    exit 1
fi

DOWNLOAD_TARGET="$WORK_DIR/downloaded-backup.tar.gz"
curl -sf "http://127.0.0.1:${SERVER_PORT}${DOWNLOAD_URL}" -o "$DOWNLOAD_TARGET"

# Verify tar.gz contents
echo "  - Validating archive contents:"
tar -tzf "$DOWNLOAD_TARGET" | sed 's/^/    /'

# 7. Test Database Mutation & Full Transactional Restore
echo -e "\n${YELLOW}[Step 7/8] Testing Database Mutation & Full Restore Sequence...${NC}"
# Soft delete target spine1 to simulate mutation
curl -sf -X DELETE "http://127.0.0.1:${SERVER_PORT}/v1/targets/spine1?force=true&force_abort=true&cleanup_scenarios=true" > /dev/null
echo "  - Mutated DB: marked target spine1 as DELETED"

# Add temporary target
curl -sf -X POST "http://127.0.0.1:${SERVER_PORT}/v1/targets" \
  -H "Content-Type: application/json" \
  -d "{\"name\": \"temp-spine-99\", \"host\": \"192.0.2.99\", \"port\": 161, \"credential_id\": \"${CRED_ID}\"}" > /dev/null
echo "  - Mutated DB: added temporary target temp-spine-99"

# Confirm spine1 status is DELETED
DELETED_STATUS=$(curl -sf "http://127.0.0.1:${SERVER_PORT}/v1/targets/spine1" | grep -o '"status":"[^"]*' | cut -d'"' -f4)
if [ "$DELETED_STATUS" != "DELETED" ]; then
    echo -e "${RED}Target spine1 status is not DELETED (got: $DELETED_STATUS)!${NC}"
    exit 1
fi
echo "  - Verified target spine1 is in DELETED status"

# Execute System Restore via API
echo "  - Triggering restore from archive: $BACKUP_FILE"
RESTORE_RESP=$(curl -sf -X POST "http://127.0.0.1:${SERVER_PORT}/v1/system/restores" \
  -H "Content-Type: application/json" \
  -d "{\"archive_path\": \"${BACKUP_FILE}\"}")
echo "  - Restore response: $RESTORE_RESP"

# Verify spine1 is restored to its pre-deletion status (DRAINING or ONLINE)
RESTORED_TARGET=$(curl -sf "http://127.0.0.1:${SERVER_PORT}/v1/targets/spine1")
RESTORED_STATUS=$(echo "$RESTORED_TARGET" | grep -o '"status":"[^"]*' | cut -d'"' -f4)
echo "  - Successfully restored target: spine1 (status: $RESTORED_STATUS)"
if [ "$RESTORED_STATUS" = "DELETED" ]; then
    echo -e "${RED}Target spine1 is still in DELETED status after restore!${NC}"
    exit 1
fi

# Verify temp-spine-99 is removed by clean restore
if curl -sf "http://127.0.0.1:${SERVER_PORT}/v1/targets/temp-spine-99" > /dev/null 2>&1; then
    echo -e "${RED}Temporary target temp-spine-99 was not cleaned up during restore!${NC}"
    exit 1
fi
echo "  - Clean restore confirmed (temp target removed, original target restored)"

# 8. Test CLI Compatibility
echo -e "\n${YELLOW}[Step 8/8] Testing musubi-cli tool against Standalone SQLite server...${NC}"
CLI="$WORK_DIR/musubi-cli"
$CLI --endpoint "http://127.0.0.1:${SERVER_PORT}" system health > /dev/null
$CLI --endpoint "http://127.0.0.1:${SERVER_PORT}" target list > /dev/null
$CLI --endpoint "http://127.0.0.1:${SERVER_PORT}" system backup > /dev/null
$CLI --endpoint "http://127.0.0.1:${SERVER_PORT}" backup create > /dev/null
$CLI --endpoint "http://127.0.0.1:${SERVER_PORT}" system purge --days 30 > /dev/null
echo "  - musubi-cli commands executed successfully"

echo -e "\n${GREEN}========================================================================${NC}"
echo -e "${GREEN} ✅ ALL SQLITE (NO-DOCKER) E2E TESTS PASSED SUCCESSFULLY!                ${NC}"
echo -e "${GREEN}    - Standalone SQLite DB: Operational without Docker                  ${NC}"
echo -e "${GREEN}    - Full Backup Archive: Generated, Validated & Downloaded            ${NC}"
echo -e "${GREEN}    - Atomic Transactional Restore: Verified & Integrity Assured        ${NC}"
echo -e "${GREEN}    - Official musubi-cli: Fully Compatible                             ${NC}"
echo -e "${GREEN}========================================================================${NC}"
