package sim

import "hash/fnv"

// traceHash is an incremental FNV-1a 64-bit hash over fixed-width
// little-endian encodings, so it matches across platforms.
type traceHash struct {
	h interface {
		Write([]byte) (int, error)
		Sum64() uint64
		Reset()
	}
	buf [8]byte
}

func newTraceHash() *traceHash { return &traceHash{h: fnv.New64a()} }

func (t *traceHash) reset() { t.h.Reset() }

func (t *traceHash) u64(v uint64) {
	for i := 0; i < 8; i++ {
		t.buf[i] = byte(v >> (8 * i))
	}
	_, _ = t.h.Write(t.buf[:])
}

func (t *traceHash) sum() uint64 { return t.h.Sum64() }
