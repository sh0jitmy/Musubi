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
	"fmt"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestMockAgent_LifecycleAndOIDs(t *testing.T) {
	t.Parallel()

	agent := NewMockAgent(map[string]any{
		"1.3.6.1.2.1.1.1.0": "RouterOS",
	})

	addr, err := agent.Start()
	require.NoError(t, err)
	assert.NotEmpty(t, addr)

	val, ok := agent.GetOID("1.3.6.1.2.1.1.1.0")
	assert.True(t, ok)
	assert.Equal(t, "RouterOS", val)

	agent.SetOID("1.3.6.1.2.1.1.5.0", "spine1")
	val, ok = agent.GetOID("1.3.6.1.2.1.1.5.0")
	assert.True(t, ok)
	assert.Equal(t, "spine1", val)

	agent.Stop()
}

func TestMockAgent_SNMP_ClientCommunication(t *testing.T) {
	t.Parallel()

	agent := NewMockAgent(map[string]any{
		".1.3.6.1.2.1.1.1.0":     "Mock-Switch-1",
		".1.3.6.1.2.1.2.2.1.7.1": 1,
		".1.3.6.1.2.1.2.2.1.8.1": 1,
	})

	addr, err := agent.Start()
	require.NoError(t, err)
	defer agent.Stop()

	host, portStr, err := net.SplitHostPort(addr)
	require.NoError(t, err)

	p64, err := strconv.ParseUint(portStr, 10, 16)
	require.NoError(t, err)
	p := uint16(p64)

	cli := &gosnmp.GoSNMP{
		Target:    host,
		Port:      p,
		Version:   gosnmp.Version2c,
		Community: "public",
		Timeout:   1 * time.Second,
		Retries:   1,
	}
	err = cli.Connect()
	require.NoError(t, err)
	defer func() {
		_ = cli.Conn.Close()
	}()

	// 1. Test Get
	res, err := cli.Get([]string{".1.3.6.1.2.1.1.1.0"})
	require.NoError(t, err)
	require.Len(t, res.Variables, 1)

	// 2. Test GetBulk
	bulkRes, err := cli.GetBulk([]string{".1.3.6.1.2.1.2.2.1"}, 0, 5)
	require.NoError(t, err)
	require.NotEmpty(t, bulkRes.Variables)

	// 3. Test Set
	setRes, err := cli.Set([]gosnmp.SnmpPDU{
		{Name: ".1.3.6.1.2.1.2.2.1.7.1", Type: gosnmp.Integer, Value: 2},
	})
	require.NoError(t, err)
	require.Len(t, setRes.Variables, 1)
}

func TestMockAgent_BulkWalk_2048(t *testing.T) {
	t.Parallel()

	oids := make(map[string]any, 2048)
	for i := 1; i <= 256; i++ {
		for col := 1; col <= 8; col++ {
			oids[fmt.Sprintf(".1.3.6.1.2.1.2.2.1.%d.%d", col, i)] = i * 10
		}
	}

	agent := NewMockAgent(oids)
	addr, err := agent.Start()
	require.NoError(t, err)
	defer agent.Stop()

	host, portStr, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	p64, err := strconv.ParseUint(portStr, 10, 16)
	require.NoError(t, err)

	cli := &gosnmp.GoSNMP{
		Target:         host,
		Port:           uint16(p64),
		Version:        gosnmp.Version2c,
		Community:      "public",
		Timeout:        1 * time.Second,
		Retries:        1,
		MaxRepetitions: 50,
	}
	err = cli.Connect()
	require.NoError(t, err)
	defer func() {
		_ = cli.Conn.Close()
	}()

	var count int
	err = cli.BulkWalk(".1.3.6.1.2.1.2.2.1", func(pdu gosnmp.SnmpPDU) error {
		count++
		return nil
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, 2048)
}
