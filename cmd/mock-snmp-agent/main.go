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
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gosnmp/gosnmp"
)

func main() {
	port := os.Getenv("SNMP_PORT")
	if port == "" {
		port = "161"
	}

	trapTarget := os.Getenv("TRAP_TARGET")
	if trapTarget == "" {
		trapTarget = "musubi-server:162"
	}

	addr, err := net.ResolveUDPAddr("udp", ":"+port)
	if err != nil {
		log.Fatalf("Failed to resolve UDP address: %v", err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		//nolint:gosec // mock test agent env log
		log.Fatalf("Failed to listen on UDP port %v: %v", port, err)
	}
	defer func() {
		_ = conn.Close()
	}()

	//nolint:gosec // mock test agent env log
	log.Printf("Mock SNMP Agent listening on UDP port %v (trap target: %v)", port, trapTarget)

	// Send periodic test Traps/Informs if requested
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			sendMockTrap(trapTarget)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	buf := make([]byte, 4096)
	go func() {
		for {
			_, _, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
		}
	}()

	<-sigChan
	log.Println("Shutting down Mock SNMP Agent...")
}

func sendMockTrap(targetAddr string) {
	host, portStr, err := net.SplitHostPort(targetAddr)
	if err != nil {
		host = "127.0.0.1"
		portStr = "162"
	}

	var port uint16 = 162
	if p, err := strconv.ParseUint(portStr, 10, 16); err == nil {
		port = uint16(p)
	}

	snmp := &gosnmp.GoSNMP{
		Target:    host,
		Port:      port,
		Version:   gosnmp.Version2c,
		Community: "public",
		Timeout:   1 * time.Second,
	}

	if connErr := snmp.Connect(); connErr != nil {
		return
	}
	defer func() {
		_ = snmp.Conn.Close()
	}()

	trap := gosnmp.SnmpTrap{
		Variables: []gosnmp.SnmpPDU{
			{
				Name:  ".1.3.6.1.2.1.2.2.1.8.1", // IF-MIB::ifOperStatus.1
				Type:  gosnmp.Integer,
				Value: 1, // up
			},
		},
	}

	_, _ = snmp.SendTrap(trap)
}
