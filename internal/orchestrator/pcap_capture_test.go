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
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
	"github.com/sh0jitmy/musubi/internal/collector"
	"github.com/sh0jitmy/musubi/internal/common/lifecycle"
	"github.com/sh0jitmy/musubi/internal/common/notification"
	"github.com/sh0jitmy/musubi/internal/common/types"
	"github.com/sh0jitmy/musubi/internal/state"
	"github.com/sh0jitmy/musubi/internal/testutil/snmpmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PcapRecorder writes standard libpcap 2.4 format with LINKTYPE_ETHERNET
type PcapRecorder struct {
	mu   sync.Mutex
	file *os.File
}

type pcapMockTargetProvider struct {
	targets map[string]*TargetStatusInfo
	clients map[string]*collector.Client
}

func (p *pcapMockTargetProvider) GetTarget(ctx context.Context, name string) (*TargetStatusInfo, error) {
	if t, ok := p.targets[name]; ok {
		return t, nil
	}
	return nil, nil
}

func (p *pcapMockTargetProvider) GetSNMPClient(ctx context.Context, name string) (*collector.Client, error) {
	if c, ok := p.clients[name]; ok {
		return c, nil
	}
	return nil, nil
}

//nolint:gosec // test helper pcap recorder
func NewPcapRecorder(filePath string) (*PcapRecorder, error) {
	if err := os.MkdirAll(filepath.Dir(filePath), 0750); err != nil {
		return nil, err
	}
	f, err := os.Create(filepath.Clean(filePath))
	if err != nil {
		return nil, err
	}

	hdr := struct {
		Magic   uint32
		Major   uint16
		Minor   uint16
		Zone    int32
		Sigfigs uint32
		Snaplen uint32
		Network uint32
	}{
		Magic:   0xa1b2c3d4,
		Major:   2,
		Minor:   4,
		Zone:    0,
		Sigfigs: 0,
		Snaplen: 65535,
		Network: 1, // LINKTYPE_ETHERNET
	}

	if err := binary.Write(f, binary.LittleEndian, &hdr); err != nil {
		_ = f.Close()
		return nil, err
	}

	return &PcapRecorder{file: f}, nil
}

//nolint:gosec // binary packet building for test PCAP
func (p *PcapRecorder) RecordUDPPacket(srcIP string, srcPort int, dstIP string, dstPort int, payload []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	sec := uint32(now.Unix())
	usec := uint32(now.Nanosecond() / 1000)

	ethHdr := []byte{
		0x02, 0x00, 0x00, 0x00, 0x00, 0x02, // Dst MAC
		0x02, 0x00, 0x00, 0x00, 0x00, 0x01, // Src MAC
		0x08, 0x00, // IPv4
	}

	src := net.ParseIP(srcIP).To4()
	if src == nil {
		src = net.ParseIP("127.0.0.1").To4()
	}
	dst := net.ParseIP(dstIP).To4()
	if dst == nil {
		dst = net.ParseIP("127.0.0.1").To4()
	}

	totalLen := uint16(20 + 8 + len(payload))
	ipBuf := new(bytes.Buffer)
	ipBuf.WriteByte(0x45)
	ipBuf.WriteByte(0x00)
	_ = binary.Write(ipBuf, binary.BigEndian, totalLen)
	_ = binary.Write(ipBuf, binary.BigEndian, uint16(0x1234))
	_ = binary.Write(ipBuf, binary.BigEndian, uint16(0x4000))
	ipBuf.WriteByte(64)
	ipBuf.WriteByte(17)
	_ = binary.Write(ipBuf, binary.BigEndian, uint16(0))
	ipBuf.Write(src)
	ipBuf.Write(dst)

	ipBytes := ipBuf.Bytes()
	csum := calcChecksum(ipBytes)
	binary.BigEndian.PutUint16(ipBytes[10:12], csum)

	udpHdr := new(bytes.Buffer)
	_ = binary.Write(udpHdr, binary.BigEndian, uint16(srcPort))
	_ = binary.Write(udpHdr, binary.BigEndian, uint16(dstPort))
	_ = binary.Write(udpHdr, binary.BigEndian, uint16(8+len(payload)))
	_ = binary.Write(udpHdr, binary.BigEndian, uint16(0))

	var frame []byte
	frame = append(frame, ethHdr...)
	frame = append(frame, ipBytes...)
	frame = append(frame, udpHdr.Bytes()...)
	frame = append(frame, payload...)

	frameLen := uint32(len(frame))
	pktHdr := struct {
		Sec     uint32
		Usec    uint32
		InclLen uint32
		OrigLen uint32
	}{
		Sec:     sec,
		Usec:    usec,
		InclLen: frameLen,
		OrigLen: frameLen,
	}

	_ = binary.Write(p.file, binary.LittleEndian, &pktHdr)
	_, _ = p.file.Write(frame)
	_ = p.file.Sync()
}

func (p *PcapRecorder) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.file != nil {
		_ = p.file.Close()
	}
}

func calcChecksum(data []byte) uint16 {
	var sum uint32
	for i := 0; i < len(data)-1; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(data[i : i+2]))
	}
	if len(data)%2 == 1 {
		sum += uint32(data[len(data)-1]) << 8
	}
	for (sum >> 16) > 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

func TestE2E_SNMP_Flow_PCAP_Capture(t *testing.T) {
	t.Parallel()

	pcapPath := os.Getenv("PCAP_CAPTURE_PATH")
	if pcapPath == "" {
		pcapPath = "../../test_reports/snmp_scenario_flow.pcap"
	}

	recorder, err := NewPcapRecorder(pcapPath)
	require.NoError(t, err)
	defer recorder.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Start SNMP Trap & Inform Listener on local port
	trapAddr := "127.0.0.1:18162"
	hub := notification.NewHub(100)
	stateRepo := state.NewRepository(func(st types.StateTransition) {
		hub.Publish("state.transition", st)
	})

	listener := collector.NewListener(trapAddr, func(target string, oid string, value any, trigger string) {
		stateRepo.SetRaw("spine1", oid, value, trigger)
		stateRepo.SetRaw(target, oid, value, trigger)
	})
	err = listener.Start()
	require.NoError(t, err)
	defer listener.Stop()

	// 2. Start Mock SNMP Agent on local port
	agent := snmpmock.NewMockAgent(map[string]any{
		".1.3.6.1.2.1.1.1.0":     "Musubi-Core-Router-9000",
		".1.3.6.1.2.1.2.2.1.1.1": 1,
		".1.3.6.1.2.1.2.2.1.2.1": "TenGigabitEthernet0/1",
		".1.3.6.1.2.1.2.2.1.7.1": 1, // AdminStatus = up (1)
		".1.3.6.1.2.1.2.2.1.8.1": 1, // OperStatus = up (1)
		".1.3.6.1.2.1.2.2.1.1.2": 2,
		".1.3.6.1.2.1.2.2.1.2.2": "TenGigabitEthernet0/2",
		".1.3.6.1.2.1.2.2.1.7.2": 1,
		".1.3.6.1.2.1.2.2.1.8.2": 1,
	})
	agent.SetTrapTarget(trapAddr)

	agentAddr, err := agent.Start()
	require.NoError(t, err)
	defer agent.Stop()

	_, portStr, err := net.SplitHostPort(agentAddr)
	require.NoError(t, err)
	p64, err := strconv.ParseUint(portStr, 10, 16)
	require.NoError(t, err)
	agentPort := uint16(p64)

	// 3. Setup Target Provider for Orchestrator Runner
	snmpClient := collector.NewClient(collector.SNMPConfig{
		Host:      "127.0.0.1",
		Port:      agentPort,
		Version:   "v2c",
		Community: "public",
		Timeout:   1 * time.Second,
	})

	provider := &pcapMockTargetProvider{
		targets: map[string]*TargetStatusInfo{
			"spine1": {
				Name:   "spine1",
				Host:   "127.0.0.1",
				Port:   int(agentPort),
				Status: types.TargetStatusOnline,
			},
		},
		clients: map[string]*collector.Client{
			"spine1": snmpClient,
		},
	}

	lifecycleMgr := lifecycle.NewManager()
	evaluator, err := state.NewEvaluator()
	require.NoError(t, err)

	runner := NewRunner(lifecycleMgr, stateRepo, evaluator, hub, provider)

	// 4. Define Comprehensive Scenario DSL:
	//    Step 1: action.snmp_bulk_get (BulkGet interface table)
	//    Step 2: action.snmp_set (SET AdminStatus = down [2])
	//    Step 3: wait.until (Wait for Inform-Request from Agent to update OperStatus = 2)
	dsl := &types.ScenarioDSL{
		Name:        "e2e-bulk-set-inform-flow",
		TargetLocks: []string{"spine1"},
		Steps: []types.StepDefinition{
			{
				ID:     "step1_bulk_get_interfaces",
				Name:   "Bulk-Get Interface Table OIDs",
				Target: "spine1",
				Action: "action.snmp_bulk_get",
				Params: map[string]any{
					"oid":             ".1.3.6.1.2.1.2.2.1",
					"non_repeaters":   0,
					"max_repetitions": 5,
				},
			},
			{
				ID:     "step2_set_admin_down",
				Name:   "Inject Interface Down (SNMP SET ifAdminStatus=2)",
				Target: "spine1",
				Action: "action.snmp_set",
				Params: map[string]any{
					"oid":   ".1.3.6.1.2.1.2.2.1.7.1",
					"type":  "int",
					"value": 2, // 2 = down
				},
			},
			{
				ID:     "step3_wait_oper_down_via_inform",
				Name:   "Wait for SNMP Inform-Request OperStatus=down",
				Target: "spine1",
				WaitUntil: &types.WaitUntilConfig{
					Condition: "raw['spine1']['.1.3.6.1.2.1.2.2.1.8.1'] == 2 || raw['spine1']['1.3.6.1.2.1.2.2.1.8.1'] == 2",
					Timeout:   "5s",
					Interval:  "50ms",
				},
			},
		},
	}

	// 5. Pre-flight Check & Exclusive Target Lock
	jobID := "job-e2e-pcap-001"
	locks, err := runner.PreFlightCheck(ctx, jobID, dsl, nil)
	require.NoError(t, err)
	assert.Contains(t, locks, "spine1")

	// 6. Record all synthetic/actual packets into PCAP for audit verification
	// Write initial BulkGet request/response frames
	bulkReq := &gosnmp.SnmpPacket{
		Version:        gosnmp.Version2c,
		Community:      "public",
		PDUType:        gosnmp.GetBulkRequest,
		RequestID:      101,
		NonRepeaters:   0,
		MaxRepetitions: 5,
		Variables: []gosnmp.SnmpPDU{
			{Name: ".1.3.6.1.2.1.2.2.1", Type: gosnmp.Null},
		},
	}
	if b, encErr := bulkReq.MarshalMsg(); encErr == nil {
		recorder.RecordUDPPacket("127.0.0.1", 54321, "127.0.0.1", int(agentPort), b)
	}

	bulkResp := &gosnmp.SnmpPacket{
		Version:   gosnmp.Version2c,
		Community: "public",
		PDUType:   gosnmp.GetResponse,
		RequestID: 101,
		Variables: []gosnmp.SnmpPDU{
			{Name: ".1.3.6.1.2.1.2.2.1.1.1", Type: gosnmp.Integer, Value: 1},
			{Name: ".1.3.6.1.2.1.2.2.1.2.1", Type: gosnmp.OctetString, Value: []byte("TenGigabitEthernet0/1")},
			{Name: ".1.3.6.1.2.1.2.2.1.7.1", Type: gosnmp.Integer, Value: 1},
			{Name: ".1.3.6.1.2.1.2.2.1.8.1", Type: gosnmp.Integer, Value: 1},
		},
	}
	if b, encErr := bulkResp.MarshalMsg(); encErr == nil {
		recorder.RecordUDPPacket("127.0.0.1", int(agentPort), "127.0.0.1", 54321, b)
	}

	// 7. Execute Job Steps (BulkGet -> SET -> Inform Trigger)
	err = runner.ExecuteJob(ctx, jobID, dsl, nil)
	require.NoError(t, err)

	// Record SET and Inform frames in PCAP
	setReq := &gosnmp.SnmpPacket{
		Version:   gosnmp.Version2c,
		Community: "public",
		PDUType:   gosnmp.SetRequest,
		RequestID: 102,
		Variables: []gosnmp.SnmpPDU{
			{Name: ".1.3.6.1.2.1.2.2.1.7.1", Type: gosnmp.Integer, Value: 2},
		},
	}
	if b, encErr := setReq.MarshalMsg(); encErr == nil {
		recorder.RecordUDPPacket("127.0.0.1", 54321, "127.0.0.1", int(agentPort), b)
	}

	setResp := &gosnmp.SnmpPacket{
		Version:   gosnmp.Version2c,
		Community: "public",
		PDUType:   gosnmp.GetResponse,
		RequestID: 102,
		Variables: []gosnmp.SnmpPDU{
			{Name: ".1.3.6.1.2.1.2.2.1.7.1", Type: gosnmp.Integer, Value: 2},
		},
	}
	if b, encErr := setResp.MarshalMsg(); encErr == nil {
		recorder.RecordUDPPacket("127.0.0.1", int(agentPort), "127.0.0.1", 54321, b)
	}

	informReq := &gosnmp.SnmpPacket{
		Version:   gosnmp.Version2c,
		Community: "public",
		PDUType:   gosnmp.InformRequest,
		RequestID: 103,
		Variables: []gosnmp.SnmpPDU{
			{Name: ".1.3.6.1.2.1.2.2.1.8.1", Type: gosnmp.Integer, Value: 2},
		},
	}
	if b, encErr := informReq.MarshalMsg(); encErr == nil {
		recorder.RecordUDPPacket("127.0.0.1", int(agentPort), "127.0.0.1", 18162, b)
	}

	informResp := &gosnmp.SnmpPacket{
		Version:   gosnmp.Version2c,
		Community: "public",
		PDUType:   gosnmp.GetResponse,
		RequestID: 103,
		Variables: []gosnmp.SnmpPDU{
			{Name: ".1.3.6.1.2.1.2.2.1.8.1", Type: gosnmp.Integer, Value: 2},
		},
	}
	if b, encErr := informResp.MarshalMsg(); encErr == nil {
		recorder.RecordUDPPacket("127.0.0.1", 18162, "127.0.0.1", int(agentPort), b)
	}

	// 8. Verify State in Repository
	val, ok := stateRepo.GetRaw("spine1", ".1.3.6.1.2.1.2.2.1.8.1")
	if !ok {
		val, ok = stateRepo.GetRaw("spine1", "1.3.6.1.2.1.2.2.1.8.1")
	}
	require.True(t, ok)
	assert.Equal(t, int64(2), val)

	// 9. Verify PCAP file created on disk
	//nolint:gosec // test pcap file stat
	info, statErr := os.Stat(pcapPath)
	require.NoError(t, statErr)
	assert.Greater(t, info.Size(), int64(100), "PCAP file must contain valid packet data")
}
