package wal

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
)

// HeaderSize is the per-record framing overhead in bytes.
const HeaderSize = 8

// MaxRecord bounds a payload; a larger length field marks a corrupt record.
const MaxRecord = 1 << 20

var castagnoli = crc32.MakeTable(crc32.Castagnoli)

// Checksum returns the CRC32C of p.
func Checksum(p []byte) uint32 { return crc32.Checksum(p, castagnoli) }

// AppendRecord appends one framed record holding payload to dst.
func AppendRecord(dst, payload []byte) []byte {
	dst = binary.LittleEndian.AppendUint32(dst, uint32(len(payload)))
	dst = binary.LittleEndian.AppendUint32(dst, Checksum(payload))
	return append(dst, payload...)
}

// Options changes how Recover treats a damaged tail.
type Options struct {
	// AcceptTornTail is bug 10 (accept_torn_tail): a record whose checksum
	// fails is accepted instead of ending recovery.
	AcceptTornTail bool
}

// Recovery is the result of scanning a log image.
type Recovery struct {
	Records      [][]byte // payloads, aliasing the input buffer
	Valid        int      // byte offset just past the last accepted record
	Truncated    int      // bytes after Valid that recovery drops
	TornAccepted bool     // a checksum-failing record was accepted (bug 10)
}

// Recover returns the records of buf up to the first invalid one. A record is
// invalid when its length is zero or above MaxRecord, when it runs past the end
// of buf, or when its checksum fails. Recover never panics.
func Recover(buf []byte, opt Options) Recovery {
	var r Recovery
	off := 0
	for off+HeaderSize <= len(buf) {
		n := int(binary.LittleEndian.Uint32(buf[off:]))
		sum := binary.LittleEndian.Uint32(buf[off+4:])
		if n == 0 || n > MaxRecord || n > len(buf)-off-HeaderSize {
			break
		}
		p := buf[off+HeaderSize : off+HeaderSize+n]
		if Checksum(p) != sum {
			if !opt.AcceptTornTail {
				break
			}
			r.Records = append(r.Records, p)
			off += HeaderSize + n
			r.TornAccepted = true
			break
		}
		r.Records = append(r.Records, p)
		off += HeaderSize + n
	}
	r.Valid = off
	r.Truncated = len(buf) - off
	return r
}

// Storage is a durable append-only byte log.
type Storage interface {
	// Append adds p at the end. It is not durable until Sync returns.
	Append(p []byte) error
	// Sync makes every appended byte durable.
	Sync() error
	// ReadAll returns the current contents.
	ReadAll() ([]byte, error)
	// Truncate shrinks the log to n bytes.
	Truncate(n int64) error
}

// ErrTruncate is returned when truncating beyond the current size.
var ErrTruncate = errors.New("wal: truncate beyond end")

// Mem is an in-memory Storage whose writes are durable at once.
type Mem struct{ buf []byte }

// Append implements Storage.
func (m *Mem) Append(p []byte) error { m.buf = append(m.buf, p...); return nil }

// Sync implements Storage.
func (m *Mem) Sync() error { return nil }

// ReadAll implements Storage.
func (m *Mem) ReadAll() ([]byte, error) { return append([]byte(nil), m.buf...), nil }

// Truncate implements Storage.
func (m *Mem) Truncate(n int64) error {
	if n < 0 || n > int64(len(m.buf)) {
		return ErrTruncate
	}
	m.buf = m.buf[:n]
	return nil
}
