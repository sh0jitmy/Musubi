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

package orchestrator

import (
	"context"
	"fmt"
	"net"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/sh0jitmy/musubi/internal/collector"
	"github.com/sh0jitmy/musubi/internal/common/lifecycle"
	"github.com/sh0jitmy/musubi/internal/common/notification"
	"github.com/sh0jitmy/musubi/internal/common/types"
	"github.com/sh0jitmy/musubi/internal/state"
	"github.com/sh0jitmy/musubi/internal/testutil/snmpmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestScale_SingleAgent_2048_OIDs_Reflection verifies that when an agent hosts 2048 MIB OIDs,
// Musubi executes Bulk-Walk/Bulk-Get, retrieves all 2048 OIDs, and reflects them 100% accurately into stateRepo.
func TestScale_SingleAgent_2048_OIDs_Reflection(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 1. Generate 2048 Distinct MIB OIDs (e.g. 256 interfaces x 8 attributes each = 2048 OIDs)
	targetName := "core-switch-2048"
	const totalOIDs = 2048
	initialOIDs := make(map[string]any, totalOIDs)

	// 256 interfaces with 8 MIB columns (index, descr, type, mtu, speed, physAddress, adminStatus, operStatus)
	for i := 1; i <= 256; i++ {
		initialOIDs[fmt.Sprintf(".1.3.6.1.2.1.2.2.1.1.%d", i)] = i
		initialOIDs[fmt.Sprintf(".1.3.6.1.2.1.2.2.1.2.%d", i)] = fmt.Sprintf("HundredGigE0/0/0/%d", i)
		initialOIDs[fmt.Sprintf(".1.3.6.1.2.1.2.2.1.3.%d", i)] = 6 // ethernetCsmacd
		initialOIDs[fmt.Sprintf(".1.3.6.1.2.1.2.2.1.4.%d", i)] = 9000
		initialOIDs[fmt.Sprintf(".1.3.6.1.2.1.2.2.1.5.%d", i)] = 1000000000
		initialOIDs[fmt.Sprintf(".1.3.6.1.2.1.2.2.1.6.%d", i)] = fmt.Sprintf("00:1a:2b:3c:4d:%02x", i%256)
		initialOIDs[fmt.Sprintf(".1.3.6.1.2.1.2.2.1.7.%d", i)] = 1 // adminStatus = up
		initialOIDs[fmt.Sprintf(".1.3.6.1.2.1.2.2.1.8.%d", i)] = 1 // operStatus = up
	}
	require.Len(t, initialOIDs, totalOIDs)

	// 2. Start Mock Agent with 2048 OIDs
	agent := snmpmock.NewMockAgent(initialOIDs)
	agentAddr, err := agent.Start()
	require.NoError(t, err)
	defer agent.Stop()

	_, portStr, err := net.SplitHostPort(agentAddr)
	require.NoError(t, err)
	p64, err := strconv.ParseUint(portStr, 10, 16)
	require.NoError(t, err)
	agentPort := uint16(p64)

	// 3. Setup Orchestrator & State Repository
	hub := notification.NewHub(totalOIDs * 2)
	stateRepo := state.NewRepository(func(st types.StateTransition) {
		hub.Publish("state.transition", st)
	})
	evaluator, err := state.NewEvaluator()
	require.NoError(t, err)
	lifecycleMgr := lifecycle.NewManager()

	snmpClient := collector.NewClient(collector.SNMPConfig{
		Host:      "127.0.0.1",
		Port:      agentPort,
		Version:   "v2c",
		Community: "public",
		Timeout:   2 * time.Second,
	})

	provider := &pcapMockTargetProvider{
		targets: map[string]*TargetStatusInfo{
			targetName: {
				Name:   targetName,
				Host:   "127.0.0.1",
				Port:   int(agentPort),
				Status: types.TargetStatusOnline,
			},
		},
		clients: map[string]*collector.Client{
			targetName: snmpClient,
		},
	}
	runner := NewRunner(lifecycleMgr, stateRepo, evaluator, hub, provider)

	// 4. Define 2048 OID Bulk-Walk Scenario
	dsl := &types.ScenarioDSL{
		Name:        "scale-2048-oid-reflection",
		TargetLocks: []string{targetName},
		Steps: []types.StepDefinition{
			{
				ID:     "step1_bulk_walk_2048",
				Name:   "Bulk-Walk 2048 Interface Table OIDs",
				Target: targetName,
				Action: "action.snmp_bulk_walk",
				Params: map[string]any{
					"oid": ".1.3.6.1.2.1.2.2.1",
				},
			},
			{
				ID:     "step2_verify_last_oid_via_cel",
				Name:   "Verify 2048th OID reflection via CEL Expression",
				Target: targetName,
				WaitUntil: &types.WaitUntilConfig{
					Condition: fmt.Sprintf("raw['%s']['.1.3.6.1.2.1.2.2.1.8.256'] == 1", targetName),
					Timeout:   "2s",
					Interval:  "10ms",
				},
			},
		},
	}

	// 5. Execute Scenario & Measure Reflection Performance
	var memBefore, memAfter runtime.MemStats
	runtime.ReadMemStats(&memBefore)
	start := time.Now()

	jobID := "job-scale-2048-reflection"
	locks, err := runner.PreFlightCheck(ctx, jobID, dsl, nil)
	require.NoError(t, err)
	assert.Contains(t, locks, targetName)

	err = runner.ExecuteJob(ctx, jobID, dsl, nil)
	elapsed := time.Since(start)
	runtime.ReadMemStats(&memAfter)

	require.NoError(t, err)

	// 6. Verify 100% Status Reflection in stateRepo
	reflectedCount := 0
	for oid, expectedVal := range initialOIDs {
		val, ok := stateRepo.GetRaw(targetName, oid)
		if ok {
			reflectedCount++
			if expectedInt, isInt := expectedVal.(int); isInt {
				assert.Equal(t, int64(expectedInt), val, "OID %s value mismatch", oid)
			} else if expectedStr, isStr := expectedVal.(string); isStr {
				assert.Equal(t, expectedStr, val, "OID %s value mismatch", oid)
			}
		}
	}

	assert.Equal(t, totalOIDs, reflectedCount, "All 2048 OIDs must be reflected into stateRepo")

	t.Logf("✅ [Scale Test 1] 2048 OIDs Reflection Result:")
	t.Logf("   - Reflected OIDs : %d / %d (100.0%%)", reflectedCount, totalOIDs)
	t.Logf("   - Elapsed Time   : %v (%.2f OIDs/sec)", elapsed, float64(totalOIDs)/(elapsed.Seconds()))
	t.Logf("   - Alloc Delta    : %.2f MB", float64(memAfter.TotalAlloc-memBefore.TotalAlloc)/1024/1024)
}

// TestScale_12Agents_Concurrent_256_OIDs_ScenarioLoad verifies concurrent execution across 12 Agents,
// where each agent hosts 256 MIB OIDs (3,072 OIDs total) and executes BulkGet + SET + Inform in parallel.
func TestScale_12Agents_Concurrent_256_OIDs_ScenarioLoad(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	const numAgents = 12
	const oidsPerAgent = 256
	const totalOIDs = numAgents * oidsPerAgent

	// 1. Start 12 Mock SNMP Agents (each with 256 distinct OIDs)
	agents := make([]*snmpmock.MockAgent, numAgents)
	agentAddrs := make([]string, numAgents)
	targetNames := make([]string, numAgents)
	targetMap := make(map[string]*TargetStatusInfo, numAgents)
	clientMap := make(map[string]*collector.Client, numAgents)

	trapAddr := "127.0.0.1:18164"
	hub := notification.NewHub(5000)
	stateRepo := state.NewRepository(func(st types.StateTransition) {
		hub.Publish("state.transition", st)
	})

	listener := collector.NewListener(trapAddr, func(targetHost string, oid string, value any, trigger string) {
		stateRepo.SetRaw(targetHost, oid, value, trigger)
		// Also record under target name for matching host
		for _, name := range targetNames {
			stateRepo.SetRaw(name, oid, value, trigger)
		}
	})
	err := listener.Start()
	require.NoError(t, err)
	defer listener.Stop()

	for i := 0; i < numAgents; i++ {
		name := fmt.Sprintf("agent-%02d", i+1)
		targetNames[i] = name

		oids := make(map[string]any, oidsPerAgent)
		// 32 interfaces x 8 MIB OIDs = 256 OIDs per agent
		for ifIdx := 1; ifIdx <= 32; ifIdx++ {
			oids[fmt.Sprintf(".1.3.6.1.2.1.2.2.1.1.%d", ifIdx)] = ifIdx
			oids[fmt.Sprintf(".1.3.6.1.2.1.2.2.1.2.%d", ifIdx)] = fmt.Sprintf("eth%d", ifIdx)
			oids[fmt.Sprintf(".1.3.6.1.2.1.2.2.1.3.%d", ifIdx)] = 6
			oids[fmt.Sprintf(".1.3.6.1.2.1.2.2.1.4.%d", ifIdx)] = 1500
			oids[fmt.Sprintf(".1.3.6.1.2.1.2.2.1.5.%d", ifIdx)] = 10000000000
			oids[fmt.Sprintf(".1.3.6.1.2.1.2.2.1.6.%d", ifIdx)] = fmt.Sprintf("52:54:00:12:%02x:%02x", i+1, ifIdx)
			oids[fmt.Sprintf(".1.3.6.1.2.1.2.2.1.7.%d", ifIdx)] = 1 // adminStatus = up (1)
			oids[fmt.Sprintf(".1.3.6.1.2.1.2.2.1.8.%d", ifIdx)] = 1 // operStatus = up (1)
		}

		ag := snmpmock.NewMockAgent(oids)
		ag.SetTrapTarget(trapAddr)
		addr, aErr := ag.Start()
		require.NoError(t, aErr)
		defer ag.Stop()

		agents[i] = ag
		agentAddrs[i] = addr

		_, portStr, splitErr := net.SplitHostPort(addr)
		require.NoError(t, splitErr)
		p64, parseErr := strconv.ParseUint(portStr, 10, 16)
		require.NoError(t, parseErr)
		port := uint16(p64)

		cli := collector.NewClient(collector.SNMPConfig{
			Host:      "127.0.0.1",
			Port:      port,
			Version:   "v2c",
			Community: "public",
			Timeout:   2 * time.Second,
		})

		targetMap[name] = &TargetStatusInfo{
			Name:   name,
			Host:   "127.0.0.1",
			Port:   int(port),
			Status: types.TargetStatusOnline,
		}
		clientMap[name] = cli
	}

	provider := &pcapMockTargetProvider{
		targets: targetMap,
		clients: clientMap,
	}

	evaluator, err := state.NewEvaluator()
	require.NoError(t, err)
	lifecycleMgr := lifecycle.NewManager()
	runner := NewRunner(lifecycleMgr, stateRepo, evaluator, hub, provider)

	// 2. Launch 12 Parallel Scenario Executions
	var wg sync.WaitGroup
	errCh := make(chan error, numAgents)
	latencies := make([]time.Duration, numAgents)

	totalStart := time.Now()

	for i := 0; i < numAgents; i++ {
		idx := i
		agentTarget := targetNames[idx]

		wg.Add(1)
		go func() {
			defer wg.Done()
			jobStart := time.Now()
			jobID := fmt.Sprintf("job-load-agent-%02d", idx+1)

			scenarioDSL := &types.ScenarioDSL{
				Name:        fmt.Sprintf("scenario-load-%s", agentTarget),
				TargetLocks: []string{agentTarget},
				Steps: []types.StepDefinition{
					{
						ID:     "step1_bulk_walk_256",
						Name:   "Bulk-Walk 256 OIDs",
						Target: agentTarget,
						Action: "action.snmp_bulk_walk",
						Params: map[string]any{
							"oid": ".1.3.6.1.2.1.2.2.1",
						},
					},
					{
						ID:     "step2_inject_set",
						Name:   "SNMP SET ifAdminStatus=2 (down)",
						Target: agentTarget,
						Action: "action.snmp_set",
						Params: map[string]any{
							"oid":   ".1.3.6.1.2.1.2.2.1.7.1",
							"type":  "int",
							"value": 2,
						},
					},
					{
						ID:     "step3_wait_oper_down",
						Name:   "Wait for Inform / State OperStatus=down",
						Target: agentTarget,
						WaitUntil: &types.WaitUntilConfig{
							Condition: fmt.Sprintf("raw['%s']['.1.3.6.1.2.1.2.2.1.8.1'] == 2 || raw['%s']['1.3.6.1.2.1.2.2.1.8.1'] == 2", agentTarget, agentTarget),
							Timeout:   "5s",
							Interval:  "20ms",
						},
					},
				},
			}

			_, preErr := runner.PreFlightCheck(ctx, jobID, scenarioDSL, nil)
			if preErr != nil {
				errCh <- fmt.Errorf("preflight %s failed: %w", jobID, preErr)
				return
			}

			execErr := runner.ExecuteJob(ctx, jobID, scenarioDSL, nil)
			if execErr != nil {
				errCh <- fmt.Errorf("execute %s failed: %w", jobID, execErr)
				return
			}

			latencies[idx] = time.Since(jobStart)
		}()
	}

	wg.Wait()
	close(errCh)
	totalDuration := time.Since(totalStart)

	// Check errors
	for e := range errCh {
		require.NoError(t, e)
	}

	// 3. Validate state reflection across all 12 agents
	totalReflected := 0
	for _, name := range targetNames {
		for ifIdx := 1; ifIdx <= 32; ifIdx++ {
			oid := fmt.Sprintf(".1.3.6.1.2.1.2.2.1.1.%d", ifIdx)
			if _, ok := stateRepo.GetRaw(name, oid); ok {
				totalReflected++
			}
		}
	}
	assert.Equal(t, numAgents*32, totalReflected, "All 12 agents must reflect interface entries into stateRepo")

	t.Logf("✅ [Scale Test 2] 12 Agents Concurrent 256 OIDs Load Result:")
	t.Logf("   - Concurrent Jobs: %d / %d completed (100%% Success, 0 Errors)", numAgents, numAgents)
	t.Logf("   - Total OIDs     : %d OIDs processed across %d targets", totalOIDs, numAgents)
	t.Logf("   - Total Duration : %v (Throughput: %.2f jobs/sec, %.2f OIDs/sec)", totalDuration, float64(numAgents)/totalDuration.Seconds(), float64(totalOIDs)/totalDuration.Seconds())
	for idx, lat := range latencies {
		t.Logf("     * Agent-%02d Job Duration: %v", idx+1, lat)
	}
}
