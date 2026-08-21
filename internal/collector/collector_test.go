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

func TestSNMP_ClientConfigAndBuilders(t *testing.T) {
	t.Parallel()

	// v3 authPriv
	cli := NewClient(SNMPConfig{
		Host:           "192.168.10.1",
		Port:           161,
		Version:        "v3",
		SecLevel:       "authPriv",
		Username:       "admin",
		AuthProtocol:   "SHA256",
		AuthPassphrase: "authpassword123",
		PrivProtocol:   "AES",
		PrivPassphrase: "privpassword123",
	})

	g, err := cli.buildGoSNMP()
	require.NoError(t, err)
	assert.Equal(t, gosnmp.Version3, g.Version)
	assert.Equal(t, gosnmp.AuthPriv, g.MsgFlags)

	// v3 authNoPriv
	cliAuthNoPriv := NewClient(SNMPConfig{
		Host:         "192.168.10.1",
		Version:      "v3",
		SecLevel:     "authNoPriv",
		Username:     "admin",
		AuthProtocol: "MD5",
	})
	gAuthNoPriv, err := cliAuthNoPriv.buildGoSNMP()
	require.NoError(t, err)
	assert.Equal(t, gosnmp.AuthNoPriv, gAuthNoPriv.MsgFlags)

	// v3 noAuthNoPriv
	cliNoAuth := NewClient(SNMPConfig{
		Host:     "192.168.10.1",
		Version:  "v3",
		SecLevel: "noAuthNoPriv",
		Username: "admin",
	})
	gNoAuth, err := cliNoAuth.buildGoSNMP()
	require.NoError(t, err)
	assert.Equal(t, gosnmp.NoAuthNoPriv, gNoAuth.MsgFlags)

	// v1
	cliV1 := NewClient(SNMPConfig{
		Host:      "192.168.10.1",
		Version:   "v1",
		Community: "public",
	})
	g1, err := cliV1.buildGoSNMP()
	require.NoError(t, err)
	assert.Equal(t, gosnmp.Version1, g1.Version)

	// v2c
	cliV2 := NewClient(SNMPConfig{
		Host:      "192.168.10.1",
		Version:   "v2c",
		Community: "public",
	})
	g2, err := cliV2.buildGoSNMP()
	require.NoError(t, err)
	assert.Equal(t, gosnmp.Version2c, g2.Version)
	assert.Equal(t, "public", g2.Community)

	// Test PDU building
	pdu, err := buildPdu(".1.3.6.1.2.1.1.1.0", "string", "hostname1")
	require.NoError(t, err)
	assert.Equal(t, gosnmp.OctetString, pdu.Type)

	pduInt, err := buildPdu(".1.3.6.1.2.1.2.2.1.8.1", "int", 1)
	require.NoError(t, err)
	assert.Equal(t, gosnmp.Integer, pduInt.Type)

	// Test parsePduValue
	val := parsePduValue(gosnmp.SnmpPDU{Type: gosnmp.OctetString, Value: []byte("router1")})
	assert.Equal(t, "router1", val)

	valInt := parsePduValue(gosnmp.SnmpPDU{Type: gosnmp.Integer, Value: 42})
	assert.Equal(t, int64(42), valInt)

	valOther := parsePduValue(gosnmp.SnmpPDU{Type: gosnmp.IPAddress, Value: "10.0.0.1"})
	assert.Equal(t, "10.0.0.1", valOther)
}

func TestSNMP_ProtocolsAndHelpers(t *testing.T) {
	t.Parallel()

	assert.Equal(t, gosnmp.MD5, parseAuthProtocol("MD5"))
	assert.Equal(t, gosnmp.SHA, parseAuthProtocol("SHA"))
	assert.Equal(t, gosnmp.SHA224, parseAuthProtocol("SHA224"))
	assert.Equal(t, gosnmp.SHA256, parseAuthProtocol("SHA256"))
	assert.Equal(t, gosnmp.SHA384, parseAuthProtocol("SHA384"))
	assert.Equal(t, gosnmp.SHA512, parseAuthProtocol("SHA512"))
	assert.Equal(t, gosnmp.SHA256, parseAuthProtocol("unknown"))

	assert.Equal(t, gosnmp.DES, parsePrivProtocol("DES"))
	assert.Equal(t, gosnmp.AES, parsePrivProtocol("AES"))
	assert.Equal(t, gosnmp.AES, parsePrivProtocol("AES128"))
	assert.Equal(t, gosnmp.AES192, parsePrivProtocol("AES192"))
	assert.Equal(t, gosnmp.AES256, parsePrivProtocol("AES256"))
	assert.Equal(t, gosnmp.AES, parsePrivProtocol("unknown"))

	// toInt conversion tests
	i1, err := toInt(10)
	require.NoError(t, err)
	assert.Equal(t, 10, i1)

	i2, err := toInt(int64(20))
	require.NoError(t, err)
	assert.Equal(t, 20, i2)

	i3, err := toInt(30.0)
	require.NoError(t, err)
	assert.Equal(t, 30, i3)

	i4, err := toInt("40")
	require.NoError(t, err)
	assert.Equal(t, 40, i4)

	_, err = toInt(struct{}{})
	require.Error(t, err)
}

func TestListener_StartStop(t *testing.T) {
	t.Parallel()

	var trapReceived bool
	l := NewListener("127.0.0.1:18162", func(target string, oid string, val any, trigger string) {
		trapReceived = true
	})

	err := l.Start()
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	l.Stop()
	assert.False(t, trapReceived)
}

func TestSNMP_ErrorPaths(t *testing.T) {
	t.Parallel()

	// Unsupported version
	invalidCli := NewClient(SNMPConfig{
		Host:    "127.0.0.1",
		Version: "v99",
	})
	_, err := invalidCli.buildGoSNMP()
	require.Error(t, err)

	// Build PDU invalid int
	_, err = buildPdu(".1.3.6.1.2.1.2.2.1.8.1", "int", "not-a-number")
	require.Error(t, err)

	// Connection failure Get
	cli := NewClient(SNMPConfig{
		Host:    "127.0.0.1",
		Port:    19999, // Unreachable port
		Timeout: 50 * time.Millisecond,
		Retries: 0,
	})
	_, err = cli.Get([]string{".1.3.6.1.2.1.1.1.0"})
	require.Error(t, err)

	// Connection failure Set
	err = cli.Set(".1.3.6.1.2.1.1.1.0", "string", "newval")
	require.Error(t, err)
}
