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

package main

import (
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/gosnmp/gosnmp"
	"github.com/sh0jitmy/musubi/internal/testutil/snmpmock"
)

func main() {
	portStr := os.Getenv("SNMP_PORT")
	if portStr == "" {
		portStr = "161"
	}
	port, _ := strconv.Atoi(portStr)

	trapTarget := os.Getenv("TRAP_TARGET")
	if trapTarget == "" {
		trapTarget = "musubi-server:162"
	}

	agent := snmpmock.NewMockAgent(map[string]any{
		".1.3.6.1.2.1.1.1.0":     "Musubi-Mock-Switch-24G",
		".1.3.6.1.2.1.1.5.0":     "spine1.datacenter.local",
		".1.3.6.1.2.1.2.2.1.1.1": 1,
		".1.3.6.1.2.1.2.2.1.2.1": "GigabitEthernet0/1",
		".1.3.6.1.2.1.2.2.1.7.1": 1, // AdminStatus = up (1)
		".1.3.6.1.2.1.2.2.1.8.1": 1, // OperStatus = up (1)
		".1.3.6.1.2.1.2.2.1.1.2": 2,
		".1.3.6.1.2.1.2.2.1.2.2": "GigabitEthernet0/2",
		".1.3.6.1.2.1.2.2.1.7.2": 1,
		".1.3.6.1.2.1.2.2.1.8.2": 1,
	})
	agent.SetTrapTarget(trapTarget)

	addr, err := agent.Start()
	if err != nil {
		log.Fatalf("Failed to start mock agent: %v", err)
	}

	//nolint:gosec // log injection not applicable for local port
	log.Printf("Mock SNMP Agent listening on UDP port %d (%s), trap target: %s", port, addr, trapTarget)

	// Send initial boot Trap
	go func() {
		_ = agent.SendTrap(trapTarget, ".1.3.6.1.2.1.2.2.1.8.1", []gosnmp.SnmpPDU{
			{
				Name:  ".1.3.6.1.2.1.2.2.1.8.1",
				Type:  gosnmp.Integer,
				Value: 1,
			},
		})
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	<-sigChan
	agent.Stop()
	log.Println("Shutting down Mock SNMP Agent...")
}
