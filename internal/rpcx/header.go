package rpcx

import (
	"encoding/binary"
	"fmt"
)

const (
	MsgTypeCall  = 0
	MsgTypeReply = 1

	NFSProgram = 100003
)

type CallHeader struct {
	XID     uint32
	MsgType uint32
	RPCVers uint32
	Program uint32
	Version uint32
	Proc    uint32
}

type ReplyHeader struct {
	XID     uint32
	MsgType uint32
}

func ParseCallHeader(payload []byte) (CallHeader, error) {
	if len(payload) < 24 {
		return CallHeader{}, fmt.Errorf("short call payload: %d", len(payload))
	}
	h := CallHeader{
		XID:     binary.BigEndian.Uint32(payload[0:4]),
		MsgType: binary.BigEndian.Uint32(payload[4:8]),
		RPCVers: binary.BigEndian.Uint32(payload[8:12]),
		Program: binary.BigEndian.Uint32(payload[12:16]),
		Version: binary.BigEndian.Uint32(payload[16:20]),
		Proc:    binary.BigEndian.Uint32(payload[20:24]),
	}
	if h.MsgType != MsgTypeCall {
		return CallHeader{}, fmt.Errorf("not a CALL message: %d", h.MsgType)
	}
	return h, nil
}

func ParseReplyHeader(payload []byte) (ReplyHeader, error) {
	if len(payload) < 8 {
		return ReplyHeader{}, fmt.Errorf("short reply payload: %d", len(payload))
	}
	h := ReplyHeader{
		XID:     binary.BigEndian.Uint32(payload[0:4]),
		MsgType: binary.BigEndian.Uint32(payload[4:8]),
	}
	if h.MsgType != MsgTypeReply {
		return ReplyHeader{}, fmt.Errorf("not a REPLY message: %d", h.MsgType)
	}
	return h, nil
}

func ProcName(proc uint32) string {
	switch proc {
	case 0:
		return "NULL"
	case 1:
		return "GETATTR"
	case 2:
		return "SETATTR"
	case 3:
		return "LOOKUP"
	case 4:
		return "ACCESS"
	case 5:
		return "READLINK"
	case 6:
		return "READ"
	case 7:
		return "WRITE"
	case 8:
		return "CREATE"
	case 9:
		return "MKDIR"
	case 10:
		return "SYMLINK"
	case 11:
		return "MKNOD"
	case 12:
		return "REMOVE"
	case 13:
		return "RMDIR"
	case 14:
		return "RENAME"
	case 15:
		return "LINK"
	case 16:
		return "READDIR"
	case 17:
		return "READDIRPLUS"
	case 18:
		return "FSSTAT"
	case 19:
		return "FSINFO"
	case 20:
		return "PATHCONF"
	case 21:
		return "COMMIT"
	default:
		return fmt.Sprintf("PROC_%d", proc)
	}
}

func MountProcName(proc uint32) string {
	switch proc {
	case 0:
		return "NULL"
	case 1:
		return "MNT"
	case 2:
		return "DUMP"
	case 3:
		return "UMNT"
	case 4:
		return "UMNTALL"
	case 5:
		return "EXPORT"
	default:
		return fmt.Sprintf("MNT_PROC_%d", proc)
	}
}

func PortmapProcName(proc uint32) string {
	switch proc {
	case 0:
		return "NULL"
	case 1:
		return "SET"
	case 2:
		return "UNSET"
	case 3:
		return "GETPORT"
	case 4:
		return "DUMP"
	case 5:
		return "CALLIT"
	default:
		return fmt.Sprintf("PMAP_PROC_%d", proc)
	}
}

func ProcNameFor(program, proc uint32) string {
	switch program {
	case NFSProgram:
		return ProcName(proc)
	case 100005:
		return MountProcName(proc)
	case 100000:
		return PortmapProcName(proc)
	default:
		return fmt.Sprintf("PROC_%d", proc)
	}
}
