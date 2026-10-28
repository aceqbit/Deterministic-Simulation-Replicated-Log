package core

import (
	"encoding/binary"

	"github.com/aceqbit/seedlog/kv"
	"github.com/aceqbit/seedlog/wal"
)

// WAL record types.
const (
	recHardState = 1
	recEntry     = 2
	recTruncate  = 3
)

const (
	hardStateSize = 1 + 8 + 8
	entrySize     = 1 + 8 + 8 + kv.CommandSize
	truncateSize  = 1 + 8
)

// EncodeBatch appends the WAL records of b to dst.
func EncodeBatch(dst []byte, b *PersistBatch) []byte {
	if b.HardState {
		off := len(dst)
		dst = append(dst, make([]byte, wal.HeaderSize)...)
		dst = append(dst, recHardState)
		dst = binary.LittleEndian.AppendUint64(dst, b.Term)
		dst = binary.LittleEndian.AppendUint64(dst, uint64(int64(b.Vote)))
		frame(dst, off)
	}
	if b.Truncate {
		off := len(dst)
		dst = append(dst, make([]byte, wal.HeaderSize)...)
		dst = append(dst, recTruncate)
		dst = binary.LittleEndian.AppendUint64(dst, b.TruncateFrom)
		frame(dst, off)
	}
	for i := range b.Entries {
		e := &b.Entries[i]
		off := len(dst)
		dst = append(dst, make([]byte, wal.HeaderSize)...)
		dst = append(dst, recEntry)
		dst = binary.LittleEndian.AppendUint64(dst, e.Term)
		dst = binary.LittleEndian.AppendUint64(dst, e.Index)
		dst = e.Cmd.AppendBinary(dst)
		frame(dst, off)
	}
	return dst
}

// frame fills the header at dst[off:] for the payload that follows it.
func frame(dst []byte, off int) {
	p := dst[off+wal.HeaderSize:]
	binary.LittleEndian.PutUint32(dst[off:], uint32(len(p)))
	binary.LittleEndian.PutUint32(dst[off+4:], wal.Checksum(p))
}

// RecoverInfo describes a WAL recovery.
type RecoverInfo struct {
	Valid        int  // bytes kept; the host truncates its log to this size
	Truncated    int  // bytes dropped after Valid
	Records      int  // records applied
	TornAccepted bool // a checksum-failing record was accepted (bug 10)
	Corrupt      bool // a record decoded to an impossible log state
	Reason       string
}

// Recover rebuilds the persistent state from a WAL image. An entry record
// with index i replaces the log suffix from i; a record that would leave a
// gap, or that does not decode, marks the image corrupt and ends recovery.
func Recover(buf []byte, acceptTornTail bool) (PersistentState, RecoverInfo) {
	r := wal.Recover(buf, wal.Options{AcceptTornTail: acceptTornTail})
	st := PersistentState{Vote: None}
	info := RecoverInfo{Valid: r.Valid, Truncated: r.Truncated, TornAccepted: r.TornAccepted}
	for _, p := range r.Records {
		if reason := applyRecord(&st, p); reason != "" {
			info.Corrupt = true
			info.Reason = reason
			break
		}
		info.Records++
	}
	return st, info
}

func applyRecord(st *PersistentState, p []byte) string {
	switch p[0] {
	case recHardState:
		if len(p) != hardStateSize {
			return "hard state size"
		}
		st.Term = binary.LittleEndian.Uint64(p[1:])
		st.Vote = int(int64(binary.LittleEndian.Uint64(p[9:])))
	case recTruncate:
		if len(p) != truncateSize {
			return "truncate size"
		}
		from := binary.LittleEndian.Uint64(p[1:])
		if from == 0 {
			return "truncate index 0"
		}
		if from <= uint64(len(st.Entries)) {
			st.Entries = st.Entries[:from-1]
		}
	case recEntry:
		if len(p) != entrySize {
			return "entry size"
		}
		cmd, err := kv.DecodeCommand(p[17:])
		if err != nil {
			return "entry command"
		}
		e := Entry{Term: binary.LittleEndian.Uint64(p[1:]), Index: binary.LittleEndian.Uint64(p[9:]), Cmd: cmd}
		n := uint64(len(st.Entries))
		if e.Index == 0 || e.Index > n+1 {
			return "entry index gap"
		}
		if e.Index <= n {
			st.Entries = st.Entries[:e.Index-1]
		}
		if len(st.Entries) > 0 && e.Term < st.Entries[len(st.Entries)-1].Term {
			return "entry term decreases"
		}
		st.Entries = append(st.Entries, e)
	default:
		return "record type"
	}
	return ""
}
