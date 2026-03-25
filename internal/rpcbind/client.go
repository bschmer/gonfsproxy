package rpcbind

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"os"
	"strings"
	"time"

	"gonfs_proxy/internal/rpcx"
)

const (
	ipProtoTCP = 6
)

// LocalRPCBindAvailable checks whether a local rpcbind/portmap TCP listener responds.
func LocalRPCBindAvailable() bool {
	_, ok := LocalRPCBindHost()
	return ok
}

// LocalRPCBindHost returns a reachable local rpcbind host literal ("127.0.0.1" or "::1").
func LocalRPCBindHost() (string, bool) {
	for _, probe := range []struct {
		host string
		addr string
	}{
		{host: "127.0.0.1", addr: "127.0.0.1:111"},
		{host: "::1", addr: "[::1]:111"},
	} {
		conn, err := net.DialTimeout("tcp", probe.addr, 1*time.Second)
		if err == nil {
			_ = conn.Close()
			return probe.host, true
		}
	}
	return "", false
}

func localOwner() string {
	hn, err := os.Hostname()
	if err == nil && strings.TrimSpace(hn) != "" {
		return hn
	}
	return "gonfs_proxy"
}

// QueryPort asks rpcbind/portmap v2 on host:rpcbindPort for program/version TCP port.
func QueryPort(host string, rpcbindPort uint16, program, version uint32) (uint16, error) {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", rpcbindPort))
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	xid := rand.New(rand.NewSource(time.Now().UnixNano())).Uint32()
	call := buildGetPortCall(xid, program, version)
	if err := rpcx.WriteRecord(conn, call); err != nil {
		return 0, err
	}

	reply, err := rpcx.ReadRecord(conn)
	if err != nil {
		return 0, err
	}
	return parseGetPortReply(reply, xid)
}

// RegisterTCPMapping registers a (program, version, tcp, port) mapping via portmap v2 SET.
func RegisterTCPMapping(host string, rpcbindPort uint16, program, version uint32, port uint16) (bool, error) {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", rpcbindPort))
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	xid := rand.New(rand.NewSource(time.Now().UnixNano())).Uint32()
	call := buildSetCall(xid, program, version, port)
	if err := rpcx.WriteRecord(conn, call); err != nil {
		return false, err
	}
	reply, err := rpcx.ReadRecord(conn)
	if err != nil {
		return false, err
	}
	return parseSetReply(reply, xid)
}

// RegisterRPCBMappingV4 registers rpcbind v4 SET mapping for netid/uaddr.
func RegisterRPCBMappingV4(host string, rpcbindPort uint16, program, version uint32, netid, uaddr string) (bool, error) {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", rpcbindPort))
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	xid := rand.New(rand.NewSource(time.Now().UnixNano())).Uint32()
	call := buildRPCBSetCallV4(xid, program, version, netid, uaddr)
	if err := rpcx.WriteRecord(conn, call); err != nil {
		return false, err
	}
	reply, err := rpcx.ReadRecord(conn)
	if err != nil {
		return false, err
	}
	return parseSetReply(reply, xid)
}

func buildGetPortCall(xid, program, version uint32) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.BigEndian, xid)
	_ = binary.Write(&b, binary.BigEndian, uint32(rpcx.MsgTypeCall))
	_ = binary.Write(&b, binary.BigEndian, uint32(2)) // rpc version
	_ = binary.Write(&b, binary.BigEndian, uint32(progPortmap))
	_ = binary.Write(&b, binary.BigEndian, uint32(2)) // portmap v2
	_ = binary.Write(&b, binary.BigEndian, uint32(3)) // GETPORT

	_ = binary.Write(&b, binary.BigEndian, uint32(authNone)) // cred flavor
	_ = binary.Write(&b, binary.BigEndian, uint32(0))        // cred len
	_ = binary.Write(&b, binary.BigEndian, uint32(authNone)) // verf flavor
	_ = binary.Write(&b, binary.BigEndian, uint32(0))        // verf len

	_ = binary.Write(&b, binary.BigEndian, program)
	_ = binary.Write(&b, binary.BigEndian, version)
	_ = binary.Write(&b, binary.BigEndian, uint32(ipProtoTCP))
	_ = binary.Write(&b, binary.BigEndian, uint32(0)) // port (unused in call)
	return b.Bytes()
}

func buildSetCall(xid, program, version uint32, port uint16) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.BigEndian, xid)
	_ = binary.Write(&b, binary.BigEndian, uint32(rpcx.MsgTypeCall))
	_ = binary.Write(&b, binary.BigEndian, uint32(2)) // rpc version
	_ = binary.Write(&b, binary.BigEndian, uint32(progPortmap))
	_ = binary.Write(&b, binary.BigEndian, uint32(2)) // portmap v2
	_ = binary.Write(&b, binary.BigEndian, uint32(1)) // SET

	_ = binary.Write(&b, binary.BigEndian, uint32(authNone)) // cred flavor
	_ = binary.Write(&b, binary.BigEndian, uint32(0))        // cred len
	_ = binary.Write(&b, binary.BigEndian, uint32(authNone)) // verf flavor
	_ = binary.Write(&b, binary.BigEndian, uint32(0))        // verf len

	_ = binary.Write(&b, binary.BigEndian, program)
	_ = binary.Write(&b, binary.BigEndian, version)
	_ = binary.Write(&b, binary.BigEndian, uint32(ipProtoTCP))
	_ = binary.Write(&b, binary.BigEndian, uint32(port))
	return b.Bytes()
}

func buildRPCBSetCallV4(xid, program, version uint32, netid, uaddr string) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.BigEndian, xid)
	_ = binary.Write(&b, binary.BigEndian, uint32(rpcx.MsgTypeCall))
	_ = binary.Write(&b, binary.BigEndian, uint32(2)) // rpc version
	_ = binary.Write(&b, binary.BigEndian, uint32(progPortmap))
	_ = binary.Write(&b, binary.BigEndian, uint32(4)) // rpcbind v4
	_ = binary.Write(&b, binary.BigEndian, uint32(1)) // SET

	_ = binary.Write(&b, binary.BigEndian, uint32(authNone)) // cred flavor
	_ = binary.Write(&b, binary.BigEndian, uint32(0))        // cred len
	_ = binary.Write(&b, binary.BigEndian, uint32(authNone)) // verf flavor
	_ = binary.Write(&b, binary.BigEndian, uint32(0))        // verf len

	_ = binary.Write(&b, binary.BigEndian, program)
	_ = binary.Write(&b, binary.BigEndian, version)
	b.Write(packXDRString(netid))
	b.Write(packXDRString(uaddr))
	b.Write(packXDRString(strings.TrimSpace(localOwner())))
	return b.Bytes()
}

func packXDRString(s string) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.BigEndian, uint32(len(s)))
	b.WriteString(s)
	pad := (4 - (len(s) % 4)) % 4
	if pad > 0 {
		b.Write(make([]byte, pad))
	}
	return b.Bytes()
}

func parseGetPortReply(payload []byte, expectXID uint32) (uint16, error) {
	p, err := parseAcceptedReplyPrefix(payload, expectXID)
	if err != nil {
		return 0, err
	}
	if p+4 > len(payload) {
		return 0, fmt.Errorf("short getport result")
	}
	port := binary.BigEndian.Uint32(payload[p : p+4])
	if port > 65535 {
		return 0, fmt.Errorf("invalid port %d", port)
	}
	return uint16(port), nil
}

func parseSetReply(payload []byte, expectXID uint32) (bool, error) {
	p, err := parseAcceptedReplyPrefix(payload, expectXID)
	if err != nil {
		return false, err
	}
	if p+4 > len(payload) {
		return false, fmt.Errorf("short set result")
	}
	v := binary.BigEndian.Uint32(payload[p : p+4])
	return v != 0, nil
}

func parseAcceptedReplyPrefix(payload []byte, expectXID uint32) (int, error) {
	if len(payload) < 28 {
		return 0, fmt.Errorf("short rpcbind reply: %d", len(payload))
	}
	if binary.BigEndian.Uint32(payload[0:4]) != expectXID {
		return 0, fmt.Errorf("xid mismatch")
	}
	if binary.BigEndian.Uint32(payload[4:8]) != rpcx.MsgTypeReply {
		return 0, fmt.Errorf("not a reply")
	}
	// reply_stat at 8..12
	if binary.BigEndian.Uint32(payload[8:12]) != msgAccepted {
		return 0, fmt.Errorf("rpc denied reply")
	}
	// accepted_reply: verf (opaque_auth) + accept_stat + results
	p := 12
	if p+8 > len(payload) {
		return 0, fmt.Errorf("short verifier header")
	}
	p += 4 // verf flavor
	vlen := int(binary.BigEndian.Uint32(payload[p : p+4]))
	p += 4
	if vlen < 0 || p+vlen > len(payload) {
		return 0, fmt.Errorf("bad verifier length")
	}
	p += vlen
	p += (4 - (vlen % 4)) % 4
	if p+4 > len(payload) {
		return 0, fmt.Errorf("short accept stat")
	}
	if binary.BigEndian.Uint32(payload[p:p+4]) != success {
		return 0, fmt.Errorf("rpc accept_stat not success")
	}
	p += 4
	return p, nil
}
