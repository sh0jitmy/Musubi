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

package pcap

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gosnmp/gosnmp"
)

// PcapRecorder writes standard libpcap 2.4 format with LINKTYPE_ETHERNET
type PcapRecorder struct {
	mu   sync.Mutex
	file *os.File
}

// Frame represents a decoded packet frame from PCAP
type Frame struct {
	Index      int
	Timestamp  time.Time
	SrcIP      string
	SrcPort    int
	DstIP      string
	DstPort    int
	Payload    []byte
	SNMPPacket *gosnmp.SnmpPacket
}

// NewPcapRecorder creates a recorder for the specified pcap file path
//
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

// RecordUDPPacket writes an Ethernet + IPv4 + UDP packet to the PCAP file
//
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

// Close closes the PCAP file
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

// ReadFrames reads all packets from a PCAP file and decodes SNMP payloads
//
//nolint:gosec // reading test pcap file
func ReadFrames(filePath string) ([]Frame, error) {
	f, err := os.Open(filepath.Clean(filePath))
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = f.Close()
	}()

	ghdr := make([]byte, 24)
	if _, err := io.ReadFull(f, ghdr); err != nil {
		return nil, fmt.Errorf("read global header: %w", err)
	}

	var frames []Frame
	idx := 1
	decoder := &gosnmp.GoSNMP{Version: gosnmp.Version2c}

	for {
		phdr := make([]byte, 16)
		if _, err := io.ReadFull(f, phdr); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return nil, fmt.Errorf("read packet header: %w", err)
		}

		sec := binary.LittleEndian.Uint32(phdr[0:4])
		usec := binary.LittleEndian.Uint32(phdr[4:8])
		inclLen := binary.LittleEndian.Uint32(phdr[8:12])

		pktData := make([]byte, inclLen)
		if _, err := io.ReadFull(f, pktData); err != nil {
			break
		}

		// Minimum Ethernet (14) + IPv4 (20) + UDP (8) = 42 bytes
		if len(pktData) < 42 {
			continue
		}

		srcIP := net.IP(pktData[26:30]).String()
		dstIP := net.IP(pktData[30:34]).String()
		srcPort := int(binary.BigEndian.Uint16(pktData[34:36]))
		dstPort := int(binary.BigEndian.Uint16(pktData[36:38]))
		payload := pktData[42:]

		var snmpPkt *gosnmp.SnmpPacket
		if len(payload) > 0 {
			if pkt, err := decoder.SnmpDecodePacket(payload); err == nil {
				snmpPkt = pkt
			} else if pkt, err := decoder.UnmarshalTrap(payload, false); err == nil {
				snmpPkt = pkt
			}
		}

		frames = append(frames, Frame{
			Index:      idx,
			Timestamp:  time.Unix(int64(sec), int64(usec)*1000),
			SrcIP:      srcIP,
			SrcPort:    srcPort,
			DstIP:      dstIP,
			DstPort:    dstPort,
			Payload:    payload,
			SNMPPacket: snmpPkt,
		})
		idx++
	}

	return frames, nil
}
