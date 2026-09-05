#!/usr/bin/env python3
import sys
import os
import socket
import struct

def decode_snmp_pdu(data):
    pdu_map = {
        0xa0: "GetRequest",
        0xa1: "GetNextRequest",
        0xa2: "GetResponse",
        0xa3: "SetRequest",
        0xa4: "Trap-v1",
        0xa5: "GetBulkRequest",
        0xa6: "InformRequest",
        0xa7: "SNMPv2-Trap",
        0xa8: "Report"
    }
    try:
        if len(data) < 10 or data[0] != 0x30:
            return {"type": "SNMP (Raw)", "details": "Non-standard encoding"}
        pos = 1
        if data[pos] & 0x80:
            pos += 1 + (data[pos] & 0x7f)
        else:
            pos += 1
        if data[pos] == 0x02:
            vlen = data[pos+1]
            pos += 2 + vlen
        if data[pos] == 0x04:
            clen = data[pos+1]
            if clen & 0x80:
                pos += 2 + (clen & 0x7f)
            else:
                pos += 2 + clen
        pdu_tag = data[pos]
        pdu_name = pdu_map.get(pdu_tag, f"PDU (0x{pdu_tag:02x})")
        return {"type": pdu_name, "tag": pdu_tag}
    except Exception as e:
        return {"type": "Unknown", "tag": 0}

def parse_pcap(pcap_path):
    if not os.path.exists(pcap_path):
        print(f"File not found: {pcap_path}")
        return
    with open(pcap_path, "rb") as f:
        ghdr = f.read(24)
        if len(ghdr) < 24:
            return
        frame_idx = 1
        print(f"=== PCAP Packet Dump: {pcap_path} ===")
        print(f"{'#':<3} | {'Source':<21} | {'Destination':<21} | {'Length':<6} | {'PDU Type'}")
        print("-" * 75)
        while True:
            phdr = f.read(16)
            if len(phdr) < 16:
                break
            sec, usec, incl_len, orig_len = struct.unpack("<IIII", phdr)
            packet_data = f.read(incl_len)
            if len(packet_data) < incl_len:
                break
            if len(packet_data) >= 42:
                src_ip = socket.inet_ntoa(packet_data[26:30])
                dst_ip = socket.inet_ntoa(packet_data[30:34])
                src_port = struct.unpack("!H", packet_data[34:36])[0]
                dst_port = struct.unpack("!H", packet_data[36:38])[0]
                udp_payload = packet_data[42:]
                pdu = decode_snmp_pdu(udp_payload)
                src = f"{src_ip}:{src_port}"
                dst = f"{dst_ip}:{dst_port}"
                print(f"{frame_idx:<3} | {src:<21} | {dst:<21} | {len(udp_payload):<6} | {pdu['type']}")
                frame_idx += 1

if __name__ == "__main__":
    path = sys.argv[1] if len(sys.argv) > 1 else "test_reports/adhoc_8step_flow.pcap"
    parse_pcap(path)
