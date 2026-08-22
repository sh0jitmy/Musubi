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
	"sync"
	"time"

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
		for {
			select {
			case <-l.ctx.Done():
				return
			default:
				_ = conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
				n, addr, err := conn.ReadFromUDP(buf)
				if err != nil {
					continue
				}
				if n > 0 && addr != nil {
					targetHost := addr.IP.String()
					telemetry.SNMPTrapCount.WithLabelValues(targetHost, "trap").Inc()
					telemetry.SNMPNetworkBytes.WithLabelValues(targetHost, "rx").Add(float64(n))
				}
			}
		}
	}()

	return nil
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
