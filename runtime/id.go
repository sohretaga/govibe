package vibe

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// ids generates UUIDv7 values (RFC 9562). Within one process the values
// strictly increase: the 12 bits after the millisecond timestamp count
// up when two IDs share a millisecond, so ordering by id follows creation
// order.
type ids struct {
	mu   sync.Mutex
	ms   int64
	seq  uint16
	now  func() time.Time
	read func([]byte) (int, error)
}

func newIDs() *ids { return &ids{now: time.Now, read: rand.Read} }

// next returns a new ID in the canonical lowercase form.
func (g *ids) next() string {
	var b [16]byte
	if _, err := g.read(b[6:]); err != nil {
		panic("vibe: no random source: " + err.Error())
	}
	g.mu.Lock()
	ms := g.now().UnixMilli()
	switch {
	case ms > g.ms:
		g.ms, g.seq = ms, uint16(b[6]&0x07)<<8|uint16(b[7])
	case g.seq < 0xfff:
		g.seq++
	default:
		g.ms++
		g.seq = 0
	}
	ms, seq := g.ms, g.seq
	g.mu.Unlock()

	for i := 5; i >= 0; i-- {
		b[i] = byte(ms)
		ms >>= 8
	}
	b[6] = 0x70 | byte(seq>>8)
	b[7] = byte(seq)
	b[8] = b[8]&0x3f | 0x80
	return format(b)
}

func format(b [16]byte) string {
	var out [36]byte
	hex.Encode(out[0:8], b[0:4])
	out[8] = '-'
	hex.Encode(out[9:13], b[4:6])
	out[13] = '-'
	hex.Encode(out[14:18], b[6:8])
	out[18] = '-'
	hex.Encode(out[19:23], b[8:10])
	out[23] = '-'
	hex.Encode(out[24:], b[10:])
	return string(out[:])
}

// ValidID reports whether s is an ID in the canonical form the runtime
// writes: 36 characters of lowercase hex and dashes.
func ValidID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				return false
			}
		}
	}
	return true
}
