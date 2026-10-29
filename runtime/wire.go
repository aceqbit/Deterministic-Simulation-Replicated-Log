package runtime

import (
	"errors"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/aceqbit/seedlog/core"
	"github.com/aceqbit/seedlog/kv"
)

// Field numbers of the Raft message in the protobuf wire format.
const (
	fType = iota + 1
	fFrom
	fTo
	fTerm
	fLastIndex
	fLastTerm
	fGranted
	fPrevIndex
	fPrevTerm
	fEntry
	fCommit
	fMatch
	fHint
)

// ErrWire is returned for a malformed message.
var ErrWire = errors.New("runtime: malformed message")

// Marshal encodes m in protobuf wire format (see the field numbers above).
func Marshal(m *core.Message) []byte {
	var b []byte
	u := func(num protowire.Number, v uint64) {
		if v != 0 {
			b = protowire.AppendTag(b, num, protowire.VarintType)
			b = protowire.AppendVarint(b, v)
		}
	}
	u(fType, uint64(m.Type))
	u(fFrom, uint64(m.From))
	u(fTo, uint64(m.To))
	u(fTerm, m.Term)
	u(fLastIndex, m.LastIndex)
	u(fLastTerm, m.LastTerm)
	if m.Granted {
		u(fGranted, 1)
	}
	u(fPrevIndex, m.PrevIndex)
	u(fPrevTerm, m.PrevTerm)
	for i := range m.Entries {
		e := &m.Entries[i]
		var eb []byte
		eb = protowire.AppendTag(eb, 1, protowire.VarintType)
		eb = protowire.AppendVarint(eb, e.Term)
		eb = protowire.AppendTag(eb, 2, protowire.VarintType)
		eb = protowire.AppendVarint(eb, e.Index)
		eb = protowire.AppendTag(eb, 3, protowire.BytesType)
		eb = protowire.AppendBytes(eb, e.Cmd.AppendBinary(nil))
		b = protowire.AppendTag(b, fEntry, protowire.BytesType)
		b = protowire.AppendBytes(b, eb)
	}
	u(fCommit, m.Commit)
	u(fMatch, m.Match)
	u(fHint, m.Hint)
	return b
}

// Unmarshal decodes a message produced by Marshal. It rejects malformed input
// and never panics.
func Unmarshal(b []byte) (core.Message, error) {
	var m core.Message
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return m, ErrWire
		}
		b = b[n:]
		if num == fEntry {
			if typ != protowire.BytesType {
				return m, ErrWire
			}
			eb, n := protowire.ConsumeBytes(b)
			if n < 0 {
				return m, ErrWire
			}
			b = b[n:]
			e, err := unmarshalEntry(eb)
			if err != nil {
				return m, err
			}
			m.Entries = append(m.Entries, e)
			continue
		}
		if typ != protowire.VarintType {
			return m, ErrWire
		}
		v, n := protowire.ConsumeVarint(b)
		if n < 0 {
			return m, ErrWire
		}
		b = b[n:]
		switch num {
		case fType:
			m.Type = core.MsgType(v)
		case fFrom:
			m.From = int(v)
		case fTo:
			m.To = int(v)
		case fTerm:
			m.Term = v
		case fLastIndex:
			m.LastIndex = v
		case fLastTerm:
			m.LastTerm = v
		case fGranted:
			m.Granted = v != 0
		case fPrevIndex:
			m.PrevIndex = v
		case fPrevTerm:
			m.PrevTerm = v
		case fCommit:
			m.Commit = v
		case fMatch:
			m.Match = v
		case fHint:
			m.Hint = v
		}
	}
	if m.Type < core.MsgRequestVote || m.Type > core.MsgAppendReply {
		return m, ErrWire
	}
	return m, nil
}

func unmarshalEntry(b []byte) (core.Entry, error) {
	var e core.Entry
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return e, ErrWire
		}
		b = b[n:]
		switch {
		case num == 3 && typ == protowire.BytesType:
			cb, n := protowire.ConsumeBytes(b)
			if n < 0 {
				return e, ErrWire
			}
			b = b[n:]
			cmd, err := kv.DecodeCommand(cb)
			if err != nil {
				return e, ErrWire
			}
			e.Cmd = cmd
		case typ == protowire.VarintType && (num == 1 || num == 2):
			v, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return e, ErrWire
			}
			b = b[n:]
			if num == 1 {
				e.Term = v
			} else {
				e.Index = v
			}
		default:
			return e, ErrWire
		}
	}
	return e, nil
}
