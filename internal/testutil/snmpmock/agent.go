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
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gosnmp/gosnmp"
)

// MockAgent simulates a network device responding to SNMP v1/v2c/v3 requests
type MockAgent struct {
	addr       string
	trapTarget string
	conn       *net.UDPConn
	oids       map[string]any
	sortedKeys []string
	packetHook func(srcIP string, srcPort int, dstIP string, dstPort int, payload []byte)
	mu         sync.RWMutex
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

// NewMockAgent creates a mock SNMP agent with initial OID values
func NewMockAgent(initialOIDs map[string]any) *MockAgent {
	ctx, cancel := context.WithCancel(context.Background())
	oidsCopy := make(map[string]any)
	for k, v := range initialOIDs {
		oidsCopy[normalizeOID(k)] = v
	}

	if _, ok := oidsCopy[".1.3.6.1.2.1.1.1.0"]; !ok {
		oidsCopy[".1.3.6.1.2.1.1.1.0"] = "Mock-SNMP-Switch-v1"
	}
	if _, ok := oidsCopy[".1.3.6.1.2.1.1.5.0"]; !ok {
		oidsCopy[".1.3.6.1.2.1.1.5.0"] = "spine1.datacenter.local"
	}
	if _, ok := oidsCopy[".1.3.6.1.2.1.2.2.1.7.1"]; !ok {
		oidsCopy[".1.3.6.1.2.1.2.2.1.7.1"] = 1 // ifAdminStatus.1 = up
	}
	if _, ok := oidsCopy[".1.3.6.1.2.1.2.2.1.8.1"]; !ok {
		oidsCopy[".1.3.6.1.2.1.2.2.1.8.1"] = 1 // ifOperStatus.1 = up
	}
	if _, ok := oidsCopy[".1.3.6.1.2.1.2.2.1.7.2"]; !ok {
		oidsCopy[".1.3.6.1.2.1.2.2.1.7.2"] = 1 // ifAdminStatus.2 = up
	}
	if _, ok := oidsCopy[".1.3.6.1.2.1.2.2.1.8.2"]; !ok {
		oidsCopy[".1.3.6.1.2.1.2.2.1.8.2"] = 1 // ifOperStatus.2 = up
	}
	if _, ok := oidsCopy[".1.3.6.1.2.1.2.2.1.10.1"]; !ok {
		oidsCopy[".1.3.6.1.2.1.2.2.1.10.1"] = int64(1048576) // ifInOctets.1
	}
	if _, ok := oidsCopy[".1.3.6.1.2.1.2.2.1.16.1"]; !ok {
		oidsCopy[".1.3.6.1.2.1.2.2.1.16.1"] = int64(2097152) // ifOutOctets.1
	}

	agent := &MockAgent{
		oids:       oidsCopy,
		trapTarget: "127.0.0.1:162",
		ctx:        ctx,
		cancel:     cancel,
	}
	agent.rebuildSortedKeys()
	return agent
}

func (m *MockAgent) rebuildSortedKeys() {
	keys := make([]string, 0, len(m.oids))
	for k := range m.oids {
		keys = append(keys, k)
	}
	sortOIDs(keys)
	m.sortedKeys = keys
}

// SetTrapTarget sets the destination address for Trap and Inform-Request messages
func (m *MockAgent) SetTrapTarget(addr string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.trapTarget = addr
}

// SetPacketHook sets a callback for capturing all UDP packets (requests, responses, informs, traps)
func (m *MockAgent) SetPacketHook(hook func(srcIP string, srcPort int, dstIP string, dstPort int, payload []byte)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.packetHook = hook
}

func (m *MockAgent) recordPacket(srcIP string, srcPort int, dstIP string, dstPort int, payload []byte) {
	hook := m.packetHook
	if hook != nil && len(payload) > 0 {
		hook(srcIP, srcPort, dstIP, dstPort, payload)
	}
}

func (m *MockAgent) port() int {
	if m.conn == nil {
		return 0
	}
	_, pStr, err := net.SplitHostPort(m.addr)
	if err != nil {
		return 0
	}
	p, _ := strconv.Atoi(pStr)
	return p
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
	norm := normalizeOID(oid)
	if _, exists := m.oids[norm]; !exists {
		m.oids[norm] = val
		m.rebuildSortedKeys()
	} else {
		m.oids[norm] = val
	}
}

// GetOID returns an OID value from the mock agent
func (m *MockAgent) GetOID(oid string) (any, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	val, ok := m.oids[normalizeOID(oid)]
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
		if p, pErr := strconv.ParseUint(portStr, 10, 16); pErr == nil {
			snmp.Port = uint16(p)
		}
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

// SendInform sends an SNMP Inform-Request packet and waits for Inform Response ACK
func (m *MockAgent) SendInform(targetAddr string, vars []gosnmp.SnmpPDU) error {
	host, portStr, err := net.SplitHostPort(targetAddr)
	if err != nil {
		host = "127.0.0.1"
		portStr = "162"
	}
	port := uint16(162)
	if p, pErr := strconv.ParseUint(portStr, 10, 16); pErr == nil {
		port = uint16(p)
	}

	snmp := &gosnmp.GoSNMP{
		Target:    host,
		Port:      port,
		Version:   gosnmp.Version2c,
		Community: "public",
		Timeout:   1 * time.Second,
	}

	rawBytes, err := snmp.SnmpEncodePacket(gosnmp.InformRequest, vars, 0, 0)
	if err != nil {
		return err
	}

	udpAddr, err := net.ResolveUDPAddr("udp", targetAddr)
	if err != nil {
		return err
	}

	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		return err
	}
	defer func() {
		_ = conn.Close()
	}()

	if _, err := conn.Write(rawBytes); err != nil {
		return err
	}
	m.recordPacket("127.0.0.1", m.port(), host, int(port), rawBytes)

	_ = conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	respBuf := make([]byte, 2048)
	respN, _ := conn.Read(respBuf)
	if respN > 0 {
		m.recordPacket(host, int(port), "127.0.0.1", m.port(), respBuf[:respN])
	}
	return nil
}

func (m *MockAgent) serve() {
	defer m.wg.Done()
	buf := make([]byte, 4096)
	decoder := &gosnmp.GoSNMP{Version: gosnmp.Version2c}

	for {
		select {
		case <-m.ctx.Done():
			return
		default:
		}

		_ = m.conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, raddr, err := m.conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}

		if n > 0 && raddr != nil {
			m.recordPacket(raddr.IP.String(), raddr.Port, "127.0.0.1", m.port(), buf[:n])
			packet, decErr := decoder.SnmpDecodePacket(buf[:n])
			if decErr != nil || packet == nil {
				packet, decErr = decoder.UnmarshalTrap(buf[:n], false)
			}
			if decErr != nil || packet == nil {
				continue
			}

			m.handlePacket(packet, raddr)
		}
	}
}

func (m *MockAgent) handlePacket(packet *gosnmp.SnmpPacket, raddr *net.UDPAddr) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var respVars []gosnmp.SnmpPDU

	switch packet.PDUType {
	case gosnmp.GetRequest:
		for _, v := range packet.Variables {
			norm := normalizeOID(v.Name)
			val, exists := m.oids[norm]
			if !exists {
				val = "0"
			}
			respVars = append(respVars, makePdu(norm, val))
		}

	case gosnmp.GetNextRequest:
		for _, v := range packet.Variables {
			prefix := normalizeOID(v.Name)
			found := false
			for _, k := range m.sortedKeys {
				if compareOIDs(k, prefix) > 0 {
					respVars = append(respVars, makePdu(k, m.oids[k]))
					found = true
					break
				}
			}
			if !found {
				respVars = append(respVars, gosnmp.SnmpPDU{
					Name:  prefix,
					Type:  gosnmp.EndOfMibView,
					Value: nil,
				})
			}
		}

	case gosnmp.GetBulkRequest:
		for _, v := range packet.Variables {
			prefix := normalizeOID(v.Name)
			count := 0
			maxReps := int(packet.MaxRepetitions)
			if maxReps <= 0 {
				maxReps = 10
			}

			for _, k := range m.sortedKeys {
				if compareOIDs(k, prefix) > 0 {
					respVars = append(respVars, makePdu(k, m.oids[k]))
					count++
					if count >= maxReps {
						break
					}
				}
			}
			if len(respVars) == 0 {
				respVars = append(respVars, gosnmp.SnmpPDU{
					Name:  prefix,
					Type:  gosnmp.EndOfMibView,
					Value: nil,
				})
			}
		}

	case gosnmp.SetRequest:
		var changedVars []gosnmp.SnmpPDU
		for _, v := range packet.Variables {
			norm := normalizeOID(v.Name)
			val := toInterfaceValue(v)
			m.oids[norm] = val
			respVars = append(respVars, v)

			if strings.HasSuffix(norm, "1.3.6.1.2.1.2.2.1.7.1") || strings.HasSuffix(norm, "ifAdminStatus.1") {
				operOid := ".1.3.6.1.2.1.2.2.1.8.1"
				m.oids[operOid] = val
				changedVars = append(changedVars, makePdu(operOid, val))
			}
		}

		if len(changedVars) > 0 && m.trapTarget != "" {
			target := m.trapTarget
			m.wg.Add(1)
			go func() {
				defer m.wg.Done()
				select {
				case <-m.ctx.Done():
					return
				case <-time.After(20 * time.Millisecond):
					_ = m.SendInform(target, changedVars)
				}
			}()
		}
	}

	respPacket := &gosnmp.SnmpPacket{
		Version:   packet.Version,
		Community: packet.Community,
		PDUType:   gosnmp.GetResponse,
		RequestID: packet.RequestID,
		Error:     gosnmp.NoError,
		Variables: respVars,
	}

	respBytes, err := respPacket.MarshalMsg()
	if err == nil {
		_, _ = m.conn.WriteToUDP(respBytes, raddr)
		m.recordPacket("127.0.0.1", m.port(), raddr.IP.String(), raddr.Port, respBytes)
	}
}

func normalizeOID(oid string) string {
	oid = strings.TrimSpace(oid)
	if !strings.HasPrefix(oid, ".") && !strings.Contains(oid, "::") {
		oid = "." + oid
	}
	return oid
}

func makePdu(oid string, val any) gosnmp.SnmpPDU {
	pdu := gosnmp.SnmpPDU{Name: oid}
	switch v := val.(type) {
	case int:
		if v > 2147483647 || v < -2147483648 {
			pdu.Type = gosnmp.Counter64
			//nolint:gosec // validated range
			pdu.Value = uint64(v)
		} else {
			pdu.Type = gosnmp.Integer
			pdu.Value = v
		}
	case int64:
		if v > 2147483647 || v < -2147483648 {
			pdu.Type = gosnmp.Counter64
			//nolint:gosec // validated range
			pdu.Value = uint64(v)
		} else {
			pdu.Type = gosnmp.Integer
			pdu.Value = int(v)
		}
	case uint64:
		pdu.Type = gosnmp.Counter64
		pdu.Value = v
	case string:
		pdu.Type = gosnmp.OctetString
		pdu.Value = []byte(v)
	default:
		pdu.Type = gosnmp.OctetString
		pdu.Value = []byte(fmt.Sprintf("%v", v))
	}
	return pdu
}

func toInterfaceValue(pdu gosnmp.SnmpPDU) any {
	switch pdu.Type {
	case gosnmp.Integer:
		return gosnmp.ToBigInt(pdu.Value).Int64()
	case gosnmp.OctetString:
		if b, ok := pdu.Value.([]byte); ok {
			return string(b)
		}
		return fmt.Sprintf("%v", pdu.Value)
	default:
		return pdu.Value
	}
}

// compareOIDs returns -1 if a < b, 1 if a > b, 0 if a == b according to numeric ASN.1 OID hierarchy
func compareOIDs(a, b string) int {
	a = strings.TrimPrefix(strings.TrimSpace(a), ".")
	b = strings.TrimPrefix(strings.TrimSpace(b), ".")

	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")

	minLen := len(aParts)
	if len(bParts) < minLen {
		minLen = len(bParts)
	}

	for i := 0; i < minLen; i++ {
		ai, aErr := strconv.ParseUint(aParts[i], 10, 64)
		bi, bErr := strconv.ParseUint(bParts[i], 10, 64)

		if aErr == nil && bErr == nil {
			if ai < bi {
				return -1
			} else if ai > bi {
				return 1
			}
		} else {
			if aParts[i] < bParts[i] {
				return -1
			} else if aParts[i] > bParts[i] {
				return 1
			}
		}
	}

	if len(aParts) < len(bParts) {
		return -1
	} else if len(aParts) > len(bParts) {
		return 1
	}
	return 0
}

func sortOIDs(keys []string) {
	sort.Slice(keys, func(i, j int) bool {
		return compareOIDs(keys[i], keys[j]) < 0
	})
}
