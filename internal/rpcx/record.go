package rpcx

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	lastFragMask = 0x80000000
	lenMask      = 0x7fffffff
)

// ReadRecord reads one ONC RPC record from r and returns the unframed payload.
func ReadRecord(r io.Reader) ([]byte, error) {
	var out []byte
	for {
		var mark uint32
		if err := binary.Read(r, binary.BigEndian, &mark); err != nil {
			return nil, err
		}
		last := (mark & lastFragMask) != 0
		n := int(mark & lenMask)
		if n < 0 {
			return nil, fmt.Errorf("invalid fragment length: %d", n)
		}
		frag := make([]byte, n)
		if _, err := io.ReadFull(r, frag); err != nil {
			return nil, err
		}
		out = append(out, frag...)
		if last {
			return out, nil
		}
	}
}

// WriteRecord writes a payload as one ONC RPC record fragment.
func WriteRecord(w io.Writer, payload []byte) error {
	mark := uint32(len(payload)) | lastFragMask
	if err := binary.Write(w, binary.BigEndian, mark); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}
