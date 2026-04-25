package idgen

import (
	"crypto/rand"
	"encoding/binary"
	"time"
)

func New() uint64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	r := binary.BigEndian.Uint64(b[:]) & 0x3fffff
	return uint64(time.Now().UnixMilli())<<22 | r
}
