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
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"
)

// SNMPConfig configures an SNMP session
type SNMPConfig struct {
	Host           string
	Port           uint16
	Version        string // "v1", "v2c", "v3"
	Community      string
	SecLevel       string // "noAuthNoPriv", "authNoPriv", "authPriv"
	Username       string
	AuthProtocol   string // "MD5", "SHA", "SHA256"
	AuthPassphrase string
	PrivProtocol   string // "DES", "AES", "AES128"
	PrivPassphrase string
	Timeout        time.Duration
	Retries        int
}

// Client wraps gosnmp.GoSNMP
type Client struct {
	cfg SNMPConfig
}

// NewClient creates a new SNMP client
func NewClient(cfg SNMPConfig) *Client {
	if cfg.Port == 0 {
		cfg.Port = 161
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 2 * time.Second
	}
	if cfg.Retries == 0 {
		cfg.Retries = 1
	}
	return &Client{cfg: cfg}
}

func (c *Client) buildGoSNMP() (*gosnmp.GoSNMP, error) {
	snmp := &gosnmp.GoSNMP{
		Target:             c.cfg.Host,
		Port:               c.cfg.Port,
		Timeout:            c.cfg.Timeout,
		Retries:            c.cfg.Retries,
		MaxOids:            60,
		ExponentialTimeout: false,
	}

	switch strings.ToLower(c.cfg.Version) {
	case "v1":
		snmp.Version = gosnmp.Version1
		snmp.Community = c.cfg.Community
	case "v2", "v2c", "":
		snmp.Version = gosnmp.Version2c
		snmp.Community = c.cfg.Community
	case "v3":
		snmp.Version = gosnmp.Version3
		snmp.SecurityModel = gosnmp.UserSecurityModel
		sp := &gosnmp.UsmSecurityParameters{
			UserName: c.cfg.Username,
		}

		switch c.cfg.SecLevel {
		case "authPriv":
			snmp.MsgFlags = gosnmp.AuthPriv
			sp.AuthenticationProtocol = parseAuthProtocol(c.cfg.AuthProtocol)
			sp.AuthenticationPassphrase = c.cfg.AuthPassphrase
			sp.PrivacyProtocol = parsePrivProtocol(c.cfg.PrivProtocol)
			sp.PrivacyPassphrase = c.cfg.PrivPassphrase
		case "authNoPriv":
			snmp.MsgFlags = gosnmp.AuthNoPriv
			sp.AuthenticationProtocol = parseAuthProtocol(c.cfg.AuthProtocol)
			sp.AuthenticationPassphrase = c.cfg.AuthPassphrase
		default:
			snmp.MsgFlags = gosnmp.NoAuthNoPriv
		}
		snmp.SecurityParameters = sp
	default:
		return nil, fmt.Errorf("unsupported snmp version: %s", c.cfg.Version)
	}

	return snmp, nil
}

// Get performs an SNMP GET for given OIDs
func (c *Client) Get(oids []string) (map[string]any, error) {
	snmp, err := c.buildGoSNMP()
	if err != nil {
		return nil, err
	}
	if connErr := snmp.Connect(); connErr != nil {
		return nil, fmt.Errorf("snmp connect failed: %w", connErr)
	}
	defer func() {
		_ = snmp.Conn.Close()
	}()

	result, err := snmp.Get(oids)
	if err != nil {
		return nil, fmt.Errorf("snmp get failed: %w", err)
	}

	resMap := make(map[string]any)
	for _, v := range result.Variables {
		resMap[v.Name] = parsePduValue(v)
	}
	return resMap, nil
}

// Set performs an SNMP SET on an OID
func (c *Client) Set(oid string, typeStr string, val any) error {
	snmp, err := c.buildGoSNMP()
	if err != nil {
		return err
	}
	if connErr := snmp.Connect(); connErr != nil {
		return fmt.Errorf("snmp connect failed: %w", connErr)
	}
	defer func() {
		_ = snmp.Conn.Close()
	}()

	pdu, err := buildPdu(oid, typeStr, val)
	if err != nil {
		return err
	}

	_, err = snmp.Set([]gosnmp.SnmpPDU{pdu})
	if err != nil {
		return fmt.Errorf("snmp set failed: %w", err)
	}
	return nil
}

func parseAuthProtocol(p string) gosnmp.SnmpV3AuthProtocol {
	switch strings.ToUpper(p) {
	case "MD5":
		return gosnmp.MD5
	case "SHA":
		return gosnmp.SHA
	case "SHA224":
		return gosnmp.SHA224
	case "SHA256":
		return gosnmp.SHA256
	case "SHA384":
		return gosnmp.SHA384
	case "SHA512":
		return gosnmp.SHA512
	default:
		return gosnmp.SHA256
	}
}

func parsePrivProtocol(p string) gosnmp.SnmpV3PrivProtocol {
	switch strings.ToUpper(p) {
	case "DES":
		return gosnmp.DES
	case "AES", "AES128":
		return gosnmp.AES
	case "AES192":
		return gosnmp.AES192
	case "AES256":
		return gosnmp.AES256
	default:
		return gosnmp.AES
	}
}

func parsePduValue(v gosnmp.SnmpPDU) any {
	switch v.Type {
	case gosnmp.OctetString:
		b, ok := v.Value.([]byte)
		if ok {
			return string(b)
		}
		return fmt.Sprintf("%v", v.Value)
	case gosnmp.Integer:
		return gosnmp.ToBigInt(v.Value).Int64()
	default:
		return v.Value
	}
}

func buildPdu(oid string, typeStr string, val any) (gosnmp.SnmpPDU, error) {
	pdu := gosnmp.SnmpPDU{Name: oid}
	switch strings.ToLower(typeStr) {
	case "integer", "int":
		intVal, err := toInt(val)
		if err != nil {
			return pdu, err
		}
		pdu.Type = gosnmp.Integer
		pdu.Value = intVal
	case "octetstring", "string", "str":
		pdu.Type = gosnmp.OctetString
		pdu.Value = fmt.Sprintf("%v", val)
	default:
		pdu.Type = gosnmp.OctetString
		pdu.Value = fmt.Sprintf("%v", val)
	}
	return pdu, nil
}

func toInt(v any) (int, error) {
	switch val := v.(type) {
	case int:
		return val, nil
	case int64:
		return int(val), nil
	case float64:
		return int(val), nil
	case string:
		return strconv.Atoi(val)
	default:
		return 0, fmt.Errorf("cannot convert %v to int", v)
	}
}
