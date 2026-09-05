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

package collector

import (
	"context"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/gosnmp/gosnmp"
	"github.com/sh0jitmy/musubi/internal/common/telemetry"
)

// TrapHandler handles received Trap/Inform PDUs
type TrapHandler func(target string, oid string, value any, trigger string)

// Listener listens for SNMP Traps and Informs on UDP port
type Listener struct {
	addr    string
	handler TrapHandler
	mu      sync.Mutex
	conn    *net.UDPConn
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// NewListener creates a new SNMP Trap/Inform UDP listener
func NewListener(addr string, handler TrapHandler) *Listener {
	ctx, cancel := context.WithCancel(context.Background())
	return &Listener{
		addr:    addr,
		handler: handler,
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Start begins listening for SNMP Traps/Informs
func (l *Listener) Start() error {
	udpAddr, err := net.ResolveUDPAddr("udp", l.addr)
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return err
	}

	l.mu.Lock()
	l.conn = conn
	l.mu.Unlock()

	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		buf := make([]byte, 4096)
		snmpDecoder := &gosnmp.GoSNMP{Version: gosnmp.Version2c}

		for {
			select {
			case <-l.ctx.Done():
				return
			default:
				_ = conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
				n, addr, readErr := conn.ReadFromUDP(buf)
				if readErr != nil {
					continue
				}
				if n > 0 && addr != nil {
					targetHost := addr.IP.String()
					telemetry.SNMPNetworkBytes.WithLabelValues(targetHost, "rx").Add(float64(n))

					packet, parseErr := snmpDecoder.SnmpDecodePacket(buf[:n])
					if parseErr != nil || packet == nil {
						packet, parseErr = snmpDecoder.UnmarshalTrap(buf[:n], false)
					}
					if parseErr != nil || packet == nil {
						telemetry.SNMPTrapCount.WithLabelValues(targetHost, "trap").Inc()
						continue
					}

					triggerType := "TRAP"
					if packet.PDUType == gosnmp.InformRequest {
						triggerType = "INFORM"
					}

					telemetry.SNMPTrapCount.WithLabelValues(targetHost, strings.ToLower(triggerType)).Inc()

					// If InformRequest, reply with Response-PDU as required by RFC 3416
					if packet.PDUType == gosnmp.InformRequest {
						respPacket := &gosnmp.SnmpPacket{
							Version:    packet.Version,
							Community:  packet.Community,
							PDUType:    gosnmp.GetResponse,
							RequestID:  packet.RequestID,
							Error:      gosnmp.NoError,
							ErrorIndex: 0,
							Variables:  packet.Variables,
						}
						if respBytes, respErr := respPacket.MarshalMsg(); respErr == nil {
							_, _ = conn.WriteToUDP(respBytes, addr)
							telemetry.SNMPNetworkBytes.WithLabelValues(targetHost, "tx").Add(float64(len(respBytes)))
						}
					}

					// Dispatch variables to handler
					if l.handler != nil {
						for _, v := range packet.Variables {
							val := parsePduValue(v)
							l.handler(targetHost, v.Name, val, triggerType)
							trimmed := strings.TrimPrefix(v.Name, ".")
							if trimmed != v.Name {
								l.handler(targetHost, trimmed, val, triggerType)
							}
						}
					}
				}
			}
		}
	}()

	return nil
}

// Addr returns the bound local address string
func (l *Listener) Addr() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.conn != nil {
		return l.conn.LocalAddr().String()
	}
	return l.addr
}

// Stop gracefully closes the UDP listener
func (l *Listener) Stop() {
	l.cancel()
	l.mu.Lock()
	if l.conn != nil {
		_ = l.conn.Close()
	}
	l.mu.Unlock()
	l.wg.Wait()
}
