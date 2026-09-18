#!/usr/bin/env bash
# Copyright 2026 Musubi Contributors
# Licensed under the Apache License, Version 2.0 (the "License");
#
# Author: sh0jitmy

set -euo pipefail

CYAN='\033[0;36m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${CYAN}================================================================${NC}"
echo -e "${CYAN}   Musubi Standalone HTMX Frontend (No-Docker) E2E Test Suite   ${NC}"
echo -e "${CYAN}================================================================${NC}"

WORK_DIR=$(mktemp -d -t musubi-frontend-e2e-XXXXXX)
BACKUP_DIR="$WORK_DIR/backups"
mkdir -p "$BACKUP_DIR"

PCAP_DIR="$(pwd)/test_reports"
mkdir -p "$PCAP_DIR"
PCAP_FILE="$PCAP_DIR/frontend_e2e_flow.pcap"
rm -f "$PCAP_FILE"

SERVER_PORT=18080
SNMP_AGENT_PORT=10161
SNMP_TRAP_PORT=10162
WEB_PORT=18081

SERVER_PID=""
AGENT_PID=""
WEB_PID=""

cleanup() {
    echo -e "\n${YELLOW}===> [Frontend E2E Cleanup] Stopping servers and cleaning up...${NC}"
    if [ -n "$WEB_PID" ] && kill -0 "$WEB_PID" > /dev/null 2>&1; then
        kill "$WEB_PID" > /dev/null 2>&1 || true
    fi
    if [ -n "$SERVER_PID" ] && kill -0 "$SERVER_PID" > /dev/null 2>&1; then
        kill "$SERVER_PID" > /dev/null 2>&1 || true
    fi
    if [ -n "$AGENT_PID" ] && kill -0 "$AGENT_PID" > /dev/null 2>&1; then
        kill "$AGENT_PID" > /dev/null 2>&1 || true
    fi
    rm -rf "$WORK_DIR"
    echo -e "${GREEN}===> [Frontend E2E Cleanup] Cleanup complete.${NC}"
}
trap cleanup EXIT INT TERM

# 1. Build Binaries
echo -e "\n${YELLOW}[Step 1/6] Compiling binaries (musubi-server, mock-snmp-agent, musubi-web)...${NC}"
go build -o "$WORK_DIR/musubi-server" ./cmd/musubi-server
go build -o "$WORK_DIR/mock-snmp-agent" ./cmd/mock-snmp-agent
go build -o "$WORK_DIR/musubi-web" ./cmd/musubi-web
echo -e "${GREEN}Binaries successfully compiled.${NC}"

# 2. Start Mock SNMP Agent
echo -e "\n${YELLOW}[Step 2/6] Starting Mock SNMP Agent on UDP port ${SNMP_AGENT_PORT}...${NC}"
SNMP_PORT="$SNMP_AGENT_PORT" TRAP_TARGET="127.0.0.1:${SNMP_TRAP_PORT}" PCAP_CAPTURE_PATH="$PCAP_FILE" \
    "$WORK_DIR/mock-snmp-agent" > "$WORK_DIR/agent.log" 2>&1 &
AGENT_PID=$!

# 3. Start Musubi Core Server (SQLite Standalone)
echo -e "\n${YELLOW}[Step 3/6] Starting Musubi Core Server (SQLite) on port ${SERVER_PORT}...${NC}"
PORT="$SERVER_PORT" \
SNMP_TRAP_PORT="$SNMP_TRAP_PORT" \
DATABASE_DRIVER="sqlite3" \
DATABASE_DSN="$WORK_DIR/musubi.db?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)" \
BACKUP_DIR="$BACKUP_DIR" \
BACKUP_ENABLED="false" \
    "$WORK_DIR/musubi-server" > "$WORK_DIR/server.log" 2>&1 &
SERVER_PID=$!

# 4. Wait for Core Server Health
echo -e "\n${YELLOW}[Step 4/6] Waiting for Musubi Core Server to become healthy...${NC}"
for i in {1..30}; do
    if curl -sf "http://127.0.0.1:${SERVER_PORT}/v1/system/healthz" > /dev/null 2>&1; then
        echo -e "${GREEN}Musubi Core Server is ONLINE!${NC}"
        break
    fi
    if [ "$i" -eq 30 ]; then
        echo -e "${RED}Core server failed to start in time. Logs:${NC}"
        cat "$WORK_DIR/server.log"
        exit 1
    fi
    sleep 1
done

# Provision initial Credential and Target
echo "  - Provisioning initial Credential and Target spine1..."
CRED_RESP=$(curl -sf -X POST "http://127.0.0.1:${SERVER_PORT}/v1/credentials" \
  -H "Content-Type: application/json" \
  -d '{"name": "v2c-default", "version": "v2c", "community": "public"}')
CRED_ID=$(echo "$CRED_RESP" | grep -o '"id":"[^"]*' | cut -d'"' -f4)

curl -sf -X POST "http://127.0.0.1:${SERVER_PORT}/v1/targets" \
  -H "Content-Type: application/json" \
  -d "{\"name\": \"spine1\", \"host\": \"127.0.0.1\", \"port\": ${SNMP_AGENT_PORT}, \"credential_id\": \"${CRED_ID}\"}" > /dev/null

# Ping target to seed MIB state transition log
curl -sf -X POST "http://127.0.0.1:${SERVER_PORT}/v1/targets/spine1/ping" > /dev/null

# 5. Start Musubi Web Frontend Server
echo -e "\n${YELLOW}[Step 5/6] Starting Musubi Web Frontend Server on port ${WEB_PORT}...${NC}"
PORT="$WEB_PORT" \
MUSUBI_API_URL="http://127.0.0.1:${SERVER_PORT}" \
    "$WORK_DIR/musubi-web" > "$WORK_DIR/web.log" 2>&1 &
WEB_PID=$!

for i in {1..20}; do
    if curl -sf "http://127.0.0.1:${WEB_PORT}/healthz" > /dev/null 2>&1; then
        echo -e "${GREEN}Musubi Web Frontend Server is ONLINE!${NC}"
        break
    fi
    if [ "$i" -eq 20 ]; then
        echo -e "${RED}Web frontend failed to start in time. Logs:${NC}"
        cat "$WORK_DIR/web.log"
        exit 1
    fi
    sleep 0.5
done

# 6. Execute Python Frontend UI & Snapshot Verification
echo -e "\n${YELLOW}[Step 6/6] Running Headless Chrome E2E Verification & Snapshot Suite...${NC}"
WEB_URL="http://127.0.0.1:${WEB_PORT}" CORE_URL="http://127.0.0.1:${SERVER_PORT}" \
    python3 scripts/test_frontend_ui.py

echo -e "\n${GREEN}========================================================================${NC}"
echo -e "${GREEN} ✅ ALL FRONTEND (NO-DOCKER) E2E TESTS PASSED SUCCESSFULLY!             ${NC}"
echo -e "${GREEN}    - Standalone Web Server: Running without Docker                     ${NC}"
echo -e "${GREEN}    - HTMX Observability Panels: 100% Validated                         ${NC}"
echo -e "${GREEN}    - Scenario Studio: Form Submission & Live Execution Verified        ${NC}"
echo -e "${GREEN}    - Snapshots Generated: docs/images/frontend_dashboard.png           ${NC}"
echo -e "${GREEN}    - HTML Test Report: test_reports/frontend_e2e_report.html           ${NC}"
echo -e "${GREEN}========================================================================${NC}"
