package peer

import (
	"encoding/binary"
	"fmt"
)

const (
	KeepaliveMessageLen = 9
)

// Keepalive defines a message which gets exchanged between two peers
type Keepalive struct {
	Priority     uint8
	Interval     uint32
	DeadInterval uint32
}

func (ka *Keepalive) MarshalBinary() []byte {
	b := make([]byte, 9)
	p := 0
	b[p] = ka.Priority
	p++
	binary.BigEndian.PutUint32(b[p:p+4], ka.Interval)
	p += 4
	binary.BigEndian.PutUint32(b[p:p+4], ka.DeadInterval)

	return b
}

func (ka *Keepalive) UnmarshalBinary(b []byte) error {
	if len(b) != KeepaliveMessageLen {
		return fmt.Errorf("invalid byte slice length, expected %d, got %d", KeepaliveMessageLen, len(b))
	}
	m := &Keepalive{}
	p := 0
	m.Priority = b[p]
	p++
	m.Interval = binary.BigEndian.Uint32(b[p : p+4])
	p += 4
	m.DeadInterval = binary.BigEndian.Uint32(b[p : p+4])

	*ka = *m

	return nil
}
