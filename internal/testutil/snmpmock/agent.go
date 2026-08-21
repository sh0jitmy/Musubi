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

package snmpmock

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/gosnmp/gosnmp"
)

// MockAgent simulates a network device responding to SNMP v1/v2c/v3 requests
type MockAgent struct {
	addr   string
	conn   *net.UDPConn
	oids   map[string]any
	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewMockAgent creates a mock SNMP agent with initial OID values
func NewMockAgent(initialOIDs map[string]any) *MockAgent {
	ctx, cancel := context.WithCancel(context.Background())
	oidsCopy := make(map[string]any)
	for k, v := range initialOIDs {
		oidsCopy[k] = v
	}

	return &MockAgent{
		oids:   oidsCopy,
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start binds to a free UDP port and starts serving
func (m *MockAgent) Start() (string, error) {
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return "", err
	}
	m.conn = conn
	m.addr = conn.LocalAddr().String()

	m.wg.Add(1)
	go m.serve()

	return m.addr, nil
}

// Stop terminates the mock agent
func (m *MockAgent) Stop() {
	m.cancel()
	if m.conn != nil {
		_ = m.conn.Close()
	}
	m.wg.Wait()
}

// SetOID updates an in-memory OID value on the mock agent
func (m *MockAgent) SetOID(oid string, val any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.oids[oid] = val
}

// GetOID returns an OID value from the mock agent
func (m *MockAgent) GetOID(oid string) (any, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	val, ok := m.oids[oid]
	return val, ok
}

// SendTrap sends an SNMP Trap packet from the mock agent to targetAddr
func (m *MockAgent) SendTrap(targetAddr string, trapOID string, vars []gosnmp.SnmpPDU) error {
	snmp := &gosnmp.GoSNMP{
		Target:    "127.0.0.1",
		Port:      162,
		Version:   gosnmp.Version2c,
		Community: "public",
		Timeout:   1 * time.Second,
	}

	host, portStr, err := net.SplitHostPort(targetAddr)
	if err == nil {
		snmp.Target = host
		var p uint16
		_, _ = net.LookupPort("udp", portStr)
		snmp.Port = p
	}

	trapPacket := gosnmp.SnmpTrap{
		Variables: vars,
	}

	err = snmp.Connect()
	if err != nil {
		return err
	}
	defer func() {
		_ = snmp.Conn.Close()
	}()

	_, err = snmp.SendTrap(trapPacket)
	return err
}

func (m *MockAgent) serve() {
	defer m.wg.Done()
	buf := make([]byte, 4096)

	for {
		select {
		case <-m.ctx.Done():
			return
		default:
		}

		_ = m.conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		_, _, err := m.conn.ReadFrom(buf)
		if err != nil {
			continue
		}
	}
}
