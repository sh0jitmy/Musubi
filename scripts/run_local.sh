#!/usr/bin/env bash
# Copyright 2026 Musubi Contributors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# Author: sh0jitmy

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
BIN_DIR="$ROOT_DIR/bin"

mkdir -p "$ROOT_DIR/data"

echo "==> Building binaries (if needed)..."
make -C "$ROOT_DIR" build

echo "==> Starting local standalone stack (Mac / Linux / Windows WSL)..."

SNMP_PORT=10161
TRAP_PORT=10162
WEB_PORT=3001

# Auto-detect available core server port (default 8080, fallback to 18080 if 8080 is in use, e.g. by Docker)
if [ -n "${CORE_PORT:-}" ]; then
    :
elif lsof -i :8080 >/dev/null 2>&1; then
    CORE_PORT=18080
    echo "  (Note: Port 8080 is currently in use by Docker/another process. Using port $CORE_PORT for local core engine)"
else
    CORE_PORT=8080
fi

# Clean shutdown handler
cleanup() {
    trap - SIGINT SIGTERM EXIT
    echo ""
    echo "==> Stopping Musubi local stack..."
    [ -n "${WEB_PID:-}" ] && kill "$WEB_PID" 2>/dev/null || true
    [ -n "${CORE_PID:-}" ] && kill "$CORE_PID" 2>/dev/null || true
    [ -n "${AGENT_PID:-}" ] && kill "$AGENT_PID" 2>/dev/null || true
    pkill -P $$ 2>/dev/null || true
    echo "==> Stopped."
    exit 0
}
trap cleanup SIGINT SIGTERM EXIT

# 1. Mock SNMP Agent
echo "  [1/3] Starting Mock SNMP Agent on UDP $SNMP_PORT..."
PORT=$SNMP_PORT "$BIN_DIR/mock-snmp-agent" > "$ROOT_DIR/data/mock-agent.log" 2>&1 &
AGENT_PID=$!

# 2. Musubi Core Server (SQLite)
echo "  [2/3] Starting Musubi Core Engine on port $CORE_PORT..."
PORT=$CORE_PORT \
SNMP_TRAP_PORT=$TRAP_PORT \
DATABASE_DRIVER=sqlite3 \
DATABASE_DSN="file:$ROOT_DIR/data/musubi.db?cache=shared&mode=rwc" \
"$BIN_DIR/musubi-server" > "$ROOT_DIR/data/musubi-server.log" 2>&1 &
CORE_PID=$!

# Verify process is alive
sleep 0.5
if ! kill -0 $CORE_PID 2>/dev/null; then
    echo "Error: musubi-server failed to start. Logs:"
    cat "$ROOT_DIR/data/musubi-server.log"
    exit 1
fi

# Wait for Core Server to be online
for i in {1..20}; do
    if curl -sf "http://127.0.0.1:$CORE_PORT/v1/system/healthz" > /dev/null 2>&1; then
        break
    fi
    sleep 0.5
done

# Seed initial credential and target if not exists
curl -sf -X POST "http://127.0.0.1:$CORE_PORT/v1/credentials" \
    -H "Content-Type: application/json" \
    -d '{"name": "v2c-default", "version": "v2c", "community": "public"}' > /dev/null 2>&1 || true

CRED_ID=$(curl -sf "http://127.0.0.1:$CORE_PORT/v1/credentials" | grep -o '"id":"[^"]*' | head -n 1 | cut -d'"' -f4 || echo "")

if [ -n "$CRED_ID" ]; then
    curl -sf -X POST "http://127.0.0.1:$CORE_PORT/v1/targets" \
        -H "Content-Type: application/json" \
        -d "{\"name\": \"spine1\", \"host\": \"127.0.0.1\", \"port\": $SNMP_PORT, \"credential_id\": \"$CRED_ID\"}" > /dev/null 2>&1 || true
fi

# Ping target to seed telemetry
curl -sf -X POST "http://127.0.0.1:$CORE_PORT/v1/targets/spine1/ping" > /dev/null 2>&1 || true

# 3. Musubi Web Frontend
echo "  [3/3] Starting Musubi Web Frontend on port $WEB_PORT..."
PORT=$WEB_PORT \
MUSUBI_API_URL="http://127.0.0.1:$CORE_PORT" \
"$BIN_DIR/musubi-web" &
WEB_PID=$!

sleep 1

echo ""
echo "=========================================================================="
echo " 結び (Musubi) Local Standalone Stack is ONLINE!"
echo "   🌐 Web Dashboard:    http://localhost:$WEB_PORT"
echo "   ⚡ Scenario Studio:  http://localhost:$WEB_PORT/scenarios"
echo "   🔌 Core REST API:    http://localhost:$CORE_PORT/v1/system/healthz"
echo "   📊 Metrics:          http://localhost:$CORE_PORT/metrics"
echo "   🎯 Initial Target:   spine1 (127.0.0.1:$SNMP_PORT)"
echo "=========================================================================="
echo "Press Ctrl+C to stop all services."
echo ""

wait $WEB_PID
