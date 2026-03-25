package rpcbind

import (
	"bytes"
	"encoding/binary"
	"io"
	"log"
	"net"
	"strconv"
	"strings"

	"gonfs_proxy/internal/rpcx"
)

const (
	progPortmap = 100000
	progNFS     = 100003
	progMOUNT   = 100005
	protoTCP    = 6

	msgAccepted = 0
	success     = 0

	authNone = 0
)

type Server struct {
	Host4     string
	Host6     string
	RPCBPort  uint32
	NFSPort   uint32
	MountPort uint32
	Logger    *log.Logger
	Verbose   bool
}

func (s *Server) Serve(listener net.Listener) error {
	if s.Logger == nil {
		s.Logger = log.Default()
	}
	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()
	for {
		rec, err := rpcx.ReadRecord(conn)
		if err != nil {
			if err != io.EOF {
				s.Logger.Printf("rpcbind read error: %v", err)
			}
			return
		}

		h, err := rpcx.ParseCallHeader(rec)
		if err != nil {
			if s.Verbose {
				s.Logger.Printf("rpcbind parse call header failed: %v", err)
			}
			return
		}
		if s.Verbose {
			s.Logger.Printf("rpcbind call xid=%d vers=%d proc=%d", h.XID, h.Version, h.Proc)
		}
		if h.Program != progPortmap {
			if s.Verbose {
				s.Logger.Printf("rpcbind ignoring non-portmap program=%d", h.Program)
			}
			_ = rpcx.WriteRecord(conn, acceptedReply(h.XID, []byte{}))
			continue
		}

		args := rec[24:]
		if len(args) < 16 {
			return
		}
		dec := newXDRDecoder(args)
		if !dec.skipOpaqueAuth() || !dec.skipOpaqueAuth() {
			return
		}

		var result []byte
		switch h.Version {
		case 2:
			result = s.handleV2(h.Proc, dec)
		case 3, 4:
			result = s.handleV3V4(h.Proc, dec)
		default:
			if s.Verbose {
				s.Logger.Printf("rpcbind unsupported version=%d", h.Version)
			}
			result = []byte{}
		}

		if err := rpcx.WriteRecord(conn, acceptedReply(h.XID, result)); err != nil {
			return
		}
	}
}

func (s *Server) handleV2(proc uint32, dec *xdrDecoder) []byte {
	switch proc {
	case 0: // NULL
		return []byte{}
	case 3: // GETPORT
		prog, ok1 := dec.u32()
		vers, ok2 := dec.u32()
		proto, ok3 := dec.u32()
		_, ok4 := dec.u32() // caller-provided port, ignored
		if !(ok1 && ok2 && ok3 && ok4) {
			if s.Verbose {
				s.Logger.Printf("rpcbind v2 GETPORT decode failed")
			}
			return packU32(0)
		}
		if proto != 6 { // TCP only
			if s.Verbose {
				s.Logger.Printf("rpcbind v2 GETPORT non-tcp proto=%d", proto)
			}
			return packU32(0)
		}
		if s.Verbose {
			s.Logger.Printf("rpcbind v2 GETPORT program=%d version=%d => %d", prog, vers, s.lookupPort(prog, vers))
		}
		return packU32(s.lookupPort(prog, vers))
	case 4: // DUMP
		if s.Verbose {
			s.Logger.Printf("rpcbind v2 DUMP requested")
		}
		return s.packV2Dump()
	default:
		if s.Verbose {
			s.Logger.Printf("rpcbind v2 unsupported proc=%d", proc)
		}
		return packU32(0)
	}
}

func (s *Server) handleV3V4(proc uint32, dec *xdrDecoder) []byte {
	switch proc {
	case 0: // NULL
		return []byte{}
	case 3, 9: // GETADDR / GETVERSADDR
		prog, ok1 := dec.u32()
		vers, ok2 := dec.u32()
		netid, ok3 := dec.xdrString()
		_, ok4 := dec.xdrString() // r_addr
		_, ok5 := dec.xdrString() // r_owner
		if !(ok1 && ok2 && ok3 && ok4 && ok5) {
			if s.Verbose {
				s.Logger.Printf("rpcbind v%d GETADDR decode failed", 3)
			}
			return packString("")
		}
		if netid != "tcp" && netid != "tcp6" {
			if s.Verbose {
				s.Logger.Printf("rpcbind GETADDR unsupported netid=%q", netid)
			}
			return packString("")
		}
		port := s.lookupPort(prog, vers)
		if port == 0 {
			if s.Verbose {
				s.Logger.Printf("rpcbind GETADDR program=%d version=%d not found", prog, vers)
			}
			return packString("")
		}
		if netid == "tcp6" {
			if s.Verbose {
				s.Logger.Printf("rpcbind GETADDR tcp6 program=%d version=%d => host6=%q port=%d", prog, vers, s.Host6, port)
			}
			return packString(UniversalAddr(s.Host6, port))
		}
		if s.Verbose {
			s.Logger.Printf("rpcbind GETADDR tcp program=%d version=%d => host4=%q port=%d", prog, vers, s.Host4, port)
		}
		return packString(UniversalAddr(s.Host4, port))
	default:
		if s.Verbose {
			s.Logger.Printf("rpcbind v3/v4 unsupported proc=%d", proc)
		}
		return []byte{}
	}
}

func (s *Server) lookupPort(program, version uint32) uint32 {
	switch program {
	case progPortmap:
		if version == 2 || version == 3 || version == 4 {
			if s.RPCBPort != 0 {
				return s.RPCBPort
			}
			return 111
		}
	case progNFS:
		if version == 3 {
			return s.NFSPort
		}
	case progMOUNT:
		if version == 1 || version == 2 || version == 3 {
			return s.MountPort
		}
	}
	return 0
}

func (s *Server) packV2Dump() []byte {
	type mapping struct {
		prog uint32
		vers uint32
		prot uint32
		port uint32
	}

	var entries []mapping
	// Include rpcbind/portmap itself.
	rpcbPort := s.RPCBPort
	if rpcbPort == 0 {
		rpcbPort = 111
	}
	for _, v := range []uint32{2, 3, 4} {
		entries = append(entries, mapping{prog: progPortmap, vers: v, prot: protoTCP, port: rpcbPort})
	}
	if s.NFSPort != 0 {
		entries = append(entries, mapping{prog: progNFS, vers: 3, prot: protoTCP, port: s.NFSPort})
	}
	if s.MountPort != 0 {
		for _, v := range []uint32{1, 2, 3} {
			entries = append(entries, mapping{prog: progMOUNT, vers: v, prot: protoTCP, port: s.MountPort})
		}
	}

	// pmaplist is a linked-list encoded as: bool more; if more then mapping + next.
	var b bytes.Buffer
	for _, e := range entries {
		_ = binary.Write(&b, binary.BigEndian, uint32(1))
		_ = binary.Write(&b, binary.BigEndian, e.prog)
		_ = binary.Write(&b, binary.BigEndian, e.vers)
		_ = binary.Write(&b, binary.BigEndian, e.prot)
		_ = binary.Write(&b, binary.BigEndian, e.port)
	}
	_ = binary.Write(&b, binary.BigEndian, uint32(0))
	return b.Bytes()
}

func acceptedReply(xid uint32, results []byte) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.BigEndian, xid)
	_ = binary.Write(&b, binary.BigEndian, uint32(rpcx.MsgTypeReply))
	_ = binary.Write(&b, binary.BigEndian, uint32(msgAccepted))
	_ = binary.Write(&b, binary.BigEndian, uint32(authNone))
	_ = binary.Write(&b, binary.BigEndian, uint32(0))
	_ = binary.Write(&b, binary.BigEndian, uint32(success))
	b.Write(results)
	return b.Bytes()
}

func packU32(v uint32) []byte {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	return b[:]
}

func packString(s string) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.BigEndian, uint32(len(s)))
	b.WriteString(s)
	pad := (4 - (len(s) % 4)) % 4
	if pad > 0 {
		b.Write(make([]byte, pad))
	}
	return b.Bytes()
}

func UniversalAddr(host string, port uint32) string {
	if strings.TrimSpace(host) == "" {
		return ""
	}
	ip := net.ParseIP(host)
	if ip4 := ip.To4(); ip4 != nil {
		p1 := int((port >> 8) & 0xff)
		p2 := int(port & 0xff)
		return ip4.String() + "." + strconv.Itoa(p1) + "." + strconv.Itoa(p2)
	}
	ip16 := ip.To16()
	if ip16 == nil {
		return ""
	}
	parts := make([]string, 0, 18)
	for _, b := range ip16 {
		parts = append(parts, strconv.Itoa(int(b)))
	}
	parts = append(parts, strconv.Itoa(int((port>>8)&0xff)))
	parts = append(parts, strconv.Itoa(int(port&0xff)))
	return strings.Join(parts, ".")
}

type xdrDecoder struct {
	b []byte
	p int
}

func newXDRDecoder(b []byte) *xdrDecoder {
	return &xdrDecoder{b: b}
}

func (d *xdrDecoder) u32() (uint32, bool) {
	if d.p+4 > len(d.b) {
		return 0, false
	}
	v := binary.BigEndian.Uint32(d.b[d.p : d.p+4])
	d.p += 4
	return v, true
}

func (d *xdrDecoder) skipOpaqueAuth() bool {
	_, ok := d.u32() // flavor
	if !ok {
		return false
	}
	n, ok := d.u32()
	if !ok {
		return false
	}
	l := int(n)
	if l < 0 || d.p+l > len(d.b) {
		return false
	}
	d.p += l
	pad := (4 - (l % 4)) % 4
	if d.p+pad > len(d.b) {
		return false
	}
	d.p += pad
	return true
}

func (d *xdrDecoder) xdrString() (string, bool) {
	n, ok := d.u32()
	if !ok {
		return "", false
	}
	l := int(n)
	if l < 0 || d.p+l > len(d.b) {
		return "", false
	}
	s := string(d.b[d.p : d.p+l])
	d.p += l
	pad := (4 - (l % 4)) % 4
	if d.p+pad > len(d.b) {
		return "", false
	}
	d.p += pad
	return strings.TrimSpace(s), true
}
