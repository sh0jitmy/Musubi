#!/usr/bin/env python3
"""
Seed rich mock data into PostgreSQL for Musubi Grafana Dashboard verification.
Populates targets, 60+ state transition logs, 35+ scenario jobs, and 40+ audit logs
so that Grafana displays rich logs, interactive filter variables, and table pagination (Page 1 of 3).
"""

import json
import random
import subprocess
import time
from datetime import datetime, timedelta, timezone

TARGETS = [
    ("spine1", "10.0.1.1", 161, "ONLINE", "cred-v3-admin"),
    ("spine2", "10.0.1.2", 161, "ONLINE", "cred-v3-admin"),
    ("leaf1", "10.0.2.1", 161, "ONLINE", "cred-v3-admin"),
    ("leaf2", "10.0.2.2", 161, "ONLINE", "cred-v3-admin"),
    ("core-router1", "10.0.0.1", 161, "ONLINE", "cred-v3-admin"),
    ("border-gw1", "10.0.0.2", 161, "DEGRADED", "cred-v3-admin"),
]

SCENARIOS = [
    ("spine-linkdown-failover", 1),
    ("bgp-route-convergence-audit", 2),
    ("bulk-interface-telemetry-scan", 1),
    ("snmp-set-remediation-flow", 3),
    ("optical-transceiver-power-check", 1),
]

TRIGGERS = ["TRAP", "INFORM", "BULK_GET", "POLLING", "SET", "API"]
STATUSES = ["SUCCESS", "FAILED", "RUNNING", "QUEUED", "ABORTED"]

SAMPLE_OIDS = [
    ("IF-MIB::ifAdminStatus.1", "1 (up)", "2 (down)"),
    ("IF-MIB::ifOperStatus.1", "1 (up)", "2 (down)"),
    ("IF-MIB::ifInOctets.1", "104857600", "105928192"),
    ("IF-MIB::ifOutOctets.1", "209715200", "211829104"),
    ("IF-MIB::ifAdminStatus.2", "1 (up)", "1 (up)"),
    ("IF-MIB::ifOperStatus.2", "1 (up)", "1 (up)"),
    ("IP-MIB::ipAdEntAddr.10.0.1.1", "10.0.1.1", "10.0.1.1"),
    ("BGP4-MIB::bgpPeerState.192.168.10.2", "6 (established)", "1 (idle)"),
    ("OSPF-MIB::ospfNbrState.10.255.0.2", "8 (full)", "1 (down)"),
    ("SNMPv2-MIB::sysUpTime.0", "129849200", "129840000"),
    ("CISCO-ENTITY-SENSOR-MIB::entSensorValue.1", "42 (degC)", "41 (degC)"),
    ("HOST-RESOURCES-MIB::hrProcessorLoad.1", "14 (%)", "12 (%)"),
]

ACTIONS = [
    ("CREATE_TARGET", "Target spine1 registered with SNMPv3"),
    ("UPDATE_TARGET", "Target leaf2 status updated to ONLINE"),
    ("CREATE_SCENARIO", "Scenario spine-linkdown-failover registered (v1)"),
    ("RUN_JOB", "Job job-spine1-failover-01 executed by operator"),
    ("SYSTEM_PURGE", "Log retention cleaner executed: 0 records pruned"),
    ("BULK_GET_SCAN", "Bulk-Get 256 OIDs scanned from core-router1"),
    ("SNMP_SET_MUTATION", "SNMP SET ifAdminStatus=2 applied to spine2"),
    ("TRAP_RECEIVED", "SNMP LinkDown trap received from 10.0.1.1:162"),
    ("INFORM_ACK_SENT", "SNMP Inform-Request Response-PDU ACK replied"),
]

def run_psql(sql):
    cmd = ["docker", "compose", "exec", "-T", "postgres", "psql", "-U", "musubi", "-d", "musubi", "-c", sql]
    res = subprocess.run(cmd, capture_output=True, text=True)
    if res.returncode != 0:
        print(f"PSQL Error: {res.stderr}")
    return res.returncode == 0

def main():
    print("🌱 Seeding rich demonstration data into Musubi PostgreSQL database...")
    now = datetime.now(timezone.utc)

    # 1. Clean existing records for clean state
    run_psql("DELETE FROM state_transition_logs; DELETE FROM jobs; DELETE FROM audit_logs; DELETE FROM targets; DELETE FROM scenarios;")

    # 2. Insert Targets
    for name, host, port, status, cred in TARGETS:
        sql = f"""
        INSERT INTO targets (id, name, host, port, status, credential_id, labels, created_at, updated_at)
        VALUES ('tgt-{name}', '{name}', '{host}', {port}, '{status}', '{cred}', '{{"role": "switch", "site": "dc1"}}', NOW(), NOW())
        ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.status, updated_at = NOW();
        """
        run_psql(sql)
    print(f"✅ Seeded {len(TARGETS)} network target devices")

    # 3. Insert Scenarios
    for sc_name, sc_ver in SCENARIOS:
        sql = f"""
        INSERT INTO scenarios (id, name, description, current_version, created_at, updated_at)
        VALUES ('sc-{sc_name}', '{sc_name}', 'Automated scenario for {sc_name}', {sc_ver}, NOW(), NOW())
        ON CONFLICT (id) DO NOTHING;
        """
        run_psql(sql)
    print(f"✅ Seeded {len(SCENARIOS)} scenarios")

    # 4. Insert 65 State Transition Logs (triggers table pagination with 25 per page)
    state_sqls = []
    for i in range(65):
        t_delta = timedelta(minutes=random.randint(1, 180), seconds=random.randint(0, 59))
        ts = (now - t_delta).isoformat()
        target = TARGETS[i % len(TARGETS)][0]
        oid, new_val, old_val = SAMPLE_OIDS[i % len(SAMPLE_OIDS)]
        trigger = TRIGGERS[i % len(TRIGGERS)]
        state_id = f"stl-{i+1:04d}"
        state_sqls.append(f"('{state_id}', '{target}', '{oid}', '{old_val}', '{new_val}', '{trigger}', '{ts}')")

    batch_sql = "INSERT INTO state_transition_logs (id, target, state_key, old_value, new_value, trigger, created_at) VALUES " + ", ".join(state_sqls) + ";"
    run_psql(batch_sql)
    print("✅ Seeded 65 state transition records (3 pages of table pagination)")

    # 5. Insert 35 Scenario Jobs (triggers table pagination with 25 per page)
    job_sqls = []
    for i in range(35):
        t_delta = timedelta(minutes=random.randint(2, 240), seconds=random.randint(0, 59))
        ts = (now - t_delta).isoformat()
        fin_ts = (now - t_delta + timedelta(seconds=random.randint(1, 15))).isoformat()
        sc_name, sc_ver = SCENARIOS[i % len(SCENARIOS)]
        status = STATUSES[i % len(STATUSES)]
        user = "admin" if i % 3 == 0 else "operator-1"
        job_id = f"job-{sc_name[:8]}-{i+1:03d}"
        job_sqls.append(f"('{job_id}', '{sc_name}', {sc_ver}, '{status}', '{{}}', '[\"spine1\"]', '{user}', '{ts}', '{fin_ts}', '{ts}')")

    batch_job_sql = "INSERT INTO jobs (id, scenario_id, scenario_version, status, dynamic_inputs, locked_targets, triggered_by, started_at, finished_at, created_at) VALUES " + ", ".join(job_sqls) + ";"
    run_psql(batch_job_sql)
    print("✅ Seeded 35 scenario execution jobs (2 pages of table pagination)")

    # 6. Insert 45 Audit Logs (triggers table pagination with 25 per page)
    audit_sqls = []
    for i in range(45):
        t_delta = timedelta(minutes=random.randint(1, 300), seconds=random.randint(0, 59))
        ts = (now - t_delta).isoformat()
        act, desc = ACTIONS[i % len(ACTIONS)]
        user = "admin" if i % 2 == 0 else "system-worker"
        target = TARGETS[i % len(TARGETS)][0]
        audit_id = f"aud-{i+1:04d}"
        audit_sqls.append(f"('{audit_id}', '{act}', '{user}', 'ADMIN', '10.0.0.50', '{target}', 'sc-spine-failover', '{json.dumps({'details': desc})}', '{ts}')")

    batch_audit_sql = "INSERT INTO audit_logs (id, action, user_id, role, ip, target_id, scenario_id, diff, created_at) VALUES " + ", ".join(audit_sqls) + ";"
    run_psql(batch_audit_sql)
    print("✅ Seeded 45 audit trail records (2 pages of table pagination)")

    print("\n🎉 Database successfully populated with rich monitoring data!")

if __name__ == "__main__":
    main()
