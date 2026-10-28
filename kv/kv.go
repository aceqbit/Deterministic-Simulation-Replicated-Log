package kv

import (
	"encoding/binary"
	"errors"
)

// OpKind identifies a state-machine operation.
type OpKind uint8

// Operation kinds. OpNoop is the entry a new leader appends in its term.
const (
	OpNoop OpKind = iota
	OpPut
	OpGet
	OpCAS
	OpIncr
)

// String returns the lower-case operation name.
func (o OpKind) String() string {
	switch o {
	case OpNoop:
		return "noop"
	case OpPut:
		return "put"
	case OpGet:
		return "get"
	case OpCAS:
		return "cas"
	case OpIncr:
		return "incr"
	}
	return "invalid"
}

// IsWrite reports whether the operation can change state.
func (o OpKind) IsWrite() bool { return o == OpPut || o == OpCAS || o == OpIncr }

// Command is one client operation. (ClientID, Seq) identifies it across retries.
type Command struct {
	ClientID uint64
	Seq      uint64
	Op       OpKind
	Key      uint32
	Value    int64 // PUT value, CAS new value, INCR delta
	Expect   int64 // CAS expected value
}

// CommandSize is the fixed encoded size of a Command.
const CommandSize = 8 + 8 + 1 + 4 + 8 + 8

// ErrShortCommand is returned when decoding fewer than CommandSize bytes.
var ErrShortCommand = errors.New("kv: short command")

// ErrBadOp is returned when the decoded operation kind is unknown.
var ErrBadOp = errors.New("kv: bad op")

// AppendBinary appends the fixed-width little-endian encoding of c to dst.
func (c Command) AppendBinary(dst []byte) []byte {
	dst = binary.LittleEndian.AppendUint64(dst, c.ClientID)
	dst = binary.LittleEndian.AppendUint64(dst, c.Seq)
	dst = append(dst, byte(c.Op))
	dst = binary.LittleEndian.AppendUint32(dst, c.Key)
	dst = binary.LittleEndian.AppendUint64(dst, uint64(c.Value))
	dst = binary.LittleEndian.AppendUint64(dst, uint64(c.Expect))
	return dst
}

// DecodeCommand decodes a Command from the first CommandSize bytes of b.
func DecodeCommand(b []byte) (Command, error) {
	if len(b) < CommandSize {
		return Command{}, ErrShortCommand
	}
	c := Command{
		ClientID: binary.LittleEndian.Uint64(b[0:]),
		Seq:      binary.LittleEndian.Uint64(b[8:]),
		Op:       OpKind(b[16]),
		Key:      binary.LittleEndian.Uint32(b[17:]),
		Value:    int64(binary.LittleEndian.Uint64(b[21:])),
		Expect:   int64(binary.LittleEndian.Uint64(b[29:])),
	}
	if c.Op > OpIncr {
		return Command{}, ErrBadOp
	}
	return c, nil
}

// Hash is the 64-bit FNV-1a digest of the command's fixed-width encoding.
func (c Command) Hash() uint64 {
	h := uint64(14695981039346656037)
	mix := func(v uint64, n int) {
		for i := 0; i < n; i++ {
			h ^= v & 0xff
			h *= 1099511628211
			v >>= 8
		}
	}
	mix(c.ClientID, 8)
	mix(c.Seq, 8)
	mix(uint64(c.Op), 1)
	mix(uint64(c.Key), 4)
	mix(uint64(c.Value), 8)
	mix(uint64(c.Expect), 8)
	return h
}

// Result is the outcome of applying a command.
type Result struct {
	OK    bool  // CAS: swapped; every other op: true
	Value int64 // GET: value read; INCR: new value; PUT/CAS: value after apply
}

type session struct {
	seq    uint64
	result Result
}

// Store is the key/value state machine with per-client sessions.
type Store struct {
	data     map[uint32]int64
	sessions map[uint64]session
	applied  uint64
	dedup    uint64
}

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{data: make(map[uint32]int64), sessions: make(map[uint64]session)}
}

// Get returns the current value of key (0 when absent). It bypasses the log.
func (s *Store) Get(key uint32) int64 { return s.data[key] }

// Applied returns the number of commands applied, including duplicates.
func (s *Store) Applied() uint64 { return s.applied }

// DedupHits returns the number of commands answered from a session cache.
func (s *Store) DedupHits() uint64 { return s.dedup }

// LastSeq returns the highest sequence number applied for a client.
func (s *Store) LastSeq(clientID uint64) uint64 { return s.sessions[clientID].seq }

// Apply executes c. With dedup on, a command whose sequence number is not
// newer than the client's last applied one is not executed again: the cached
// result is returned for the latest sequence and dup is true. With dedup off
// (bug double_apply_retry) every command executes.
func (s *Store) Apply(c Command, dedup bool) (res Result, dup bool) {
	s.applied++
	if c.Op == OpNoop {
		return Result{OK: true}, false
	}
	if c.ClientID != 0 && dedup {
		if sess, ok := s.sessions[c.ClientID]; ok && c.Seq <= sess.seq {
			s.dedup++
			if c.Seq == sess.seq {
				return sess.result, true
			}
			return Result{}, true
		}
	}
	cur := s.data[c.Key]
	switch c.Op {
	case OpPut:
		s.data[c.Key] = c.Value
		res = Result{OK: true, Value: c.Value}
	case OpGet:
		res = Result{OK: true, Value: cur}
	case OpCAS:
		if cur == c.Expect {
			s.data[c.Key] = c.Value
			res = Result{OK: true, Value: c.Value}
		} else {
			res = Result{OK: false, Value: cur}
		}
	case OpIncr:
		s.data[c.Key] = cur + c.Value
		res = Result{OK: true, Value: cur + c.Value}
	}
	if c.ClientID != 0 {
		if sess, ok := s.sessions[c.ClientID]; !ok || c.Seq > sess.seq {
			s.sessions[c.ClientID] = session{seq: c.Seq, result: res}
		}
	}
	return res, false
}
