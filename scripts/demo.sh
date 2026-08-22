#!/usr/bin/env bash
set -euo pipefail

CYAN='\033[0;36m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${CYAN}====================================================${NC}"
echo -e "${CYAN}        Musubi Full-Stack Live Demo Runner          ${NC}"
echo -e "${CYAN}====================================================${NC}"

# 1. Start Docker Compose Stack
echo -e "\n${YELLOW}[Step 1/5] Building & Starting Docker Compose Stack...${NC}"
docker compose up -d --build

# 2. Wait for Health Checks
echo -e "\n${YELLOW}[Step 2/5] Waiting for services to be HEALTHY...${NC}"
until curl -sf http://localhost:8080/v1/system/healthz > /dev/null 2>&1; do
    echo -n "."
    sleep 2
done
echo -e " ${GREEN}[Musubi Server is ONLINE]${NC}"

until curl -sf http://localhost:8428/health > /dev/null 2>&1; do
    echo -n "."
    sleep 1
done
echo -e " ${GREEN}[VictoriaMetrics is ONLINE]${NC}"

until curl -sf http://localhost:3000/api/health > /dev/null 2>&1; do
    echo -n "."
    sleep 1
done
echo -e " ${GREEN}[Grafana is ONLINE]${NC}"

# 3. Provision Credentials and Target
echo -e "\n${YELLOW}[Step 3/5] Provisioning Target Inventory via REST API...${NC}"
curl -s -X POST http://localhost:8080/v1/credentials \
  -H "Content-Type: application/json" \
  -d '{"name": "v3-admin", "version": "v3", "sec_level": "authPriv", "username": "admin"}' | grep -o '"id":[^,]*' || true

curl -s -X POST http://localhost:8080/v1/targets \
  -H "Content-Type: application/json" \
  -d '{"name": "spine1", "host": "mock-snmp-agent", "port": 161, "credential_id": "cred-v3-admin", "labels": {"role": "spine", "site": "dc1"}}' | grep -o '"status":[^,]*' || true

# 4. Register & Run Scenario
echo -e "\n${YELLOW}[Step 4/5] Registering Scenario & Executing Scenario Job...${NC}"
SCENARIO_YAML='name: spine-linkdown-failover
target_locks:
  - spine1
steps:
  - id: step1_link_status_check
    target: spine1
    wait:
      until: "true"
      timeout: "5s"
'

curl -s -X POST http://localhost:8080/v1/scenarios \
  -H "Content-Type: application/json" \
  -d "{\"name\": \"spine-linkdown-failover\", \"dsl_yaml\": \"$(echo "$SCENARIO_YAML" | sed 's/"/\\"/g' | awk '{printf "%s\\n", $0}')\"}" | grep -o '"current_version":[^,]*' || true

echo -e "\n${CYAN}Triggering Scenario Execution...${NC}"
RUN_RESP=$(curl -s -X POST http://localhost:8080/v1/scenarios/spine-linkdown-failover/runs -H "Content-Type: application/json" -d '{}')
echo "$RUN_RESP"

# 5. Success summary
echo -e "\n${GREEN}====================================================${NC}"
echo -e "${GREEN}        🎉 Musubi Demo Stack is LIVE & READY        ${NC}"
echo -e "${GREEN}====================================================${NC}"
echo -e "🔹 ${CYAN}Musubi REST API / Health${NC}  : http://localhost:8080/v1/system/healths"
echo -e "🔹 ${CYAN}Grafana Dashboard (Overview)${NC}: http://localhost:3000/d/musubi-overview"
echo -e "🔹 ${CYAN}VictoriaMetrics Metrics TSDB${NC}: http://localhost:8428"
echo -e "🔹 ${CYAN}PostgreSQL Database${NC}         : postgresql://musubi:musubi_secret@localhost:5432/musubi"
echo -e "\nTo stop the demo: ${YELLOW}docker compose down${NC}"
