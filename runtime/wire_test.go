package runtime

import (
	"reflect"
	"testing"

	"github.com/aceqbit/seedlog/core"
	"github.com/aceqbit/seedlog/kv"
)

func sampleMessages() []core.Message {
	return []core.Message{
		{Type: core.MsgRequestVote, From: 1, To: 2, Term: 9, LastIndex: 4, LastTerm: 3},
		{Type: core.MsgVoteReply, From: 2, To: 1, Term: 9, Granted: true},
		{Type: core.MsgAppend, From: 0, To: 3, Term: 5, PrevIndex: 7, PrevTerm: 4, Commit: 6,
			Entries: []core.Entry{{Term: 5, Index: 8, Cmd: kv.Command{ClientID: 3, Seq: 2, Op: kv.OpCAS, Key: 4, Value: -5, Expect: 6}}, {Term: 5, Index: 9}}},
		{Type: core.MsgAppendReply, From: 3, To: 0, Term: 5, Granted: true, Match: 9},
		{Type: core.MsgAppendReply, From: 3, To: 0, Term: 5, Hint: 2},
	}
}

func TestWireRoundTrip(t *testing.T) {
	for i, m := range sampleMessages() {
		got, err := Unmarshal(Marshal(&m))
		if err != nil {
			t.Fatalf("%d: %v", i, err)
		}
		if !reflect.DeepEqual(got, m) {
			t.Fatalf("%d: got %+v want %+v", i, got, m)
		}
	}
}

func TestWireRejectsGarbage(t *testing.T) {
	for _, b := range [][]byte{nil, {0xff}, {0x08}, {0x08, 0x09}, {0x52, 0x05, 1, 2}} {
		if _, err := Unmarshal(b); err == nil {
			t.Errorf("accepted %x", b)
		}
	}
}

// FuzzWire checks struct to bytes to struct and that decoding never panics.
func FuzzWire(f *testing.F) {
	for _, m := range sampleMessages() {
		f.Add(Marshal(&m))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		m, err := Unmarshal(b)
		if err != nil {
			return
		}
		again, err := Unmarshal(Marshal(&m))
		if err != nil || !reflect.DeepEqual(again, m) {
			t.Fatalf("round trip changed %+v -> %+v (%v)", m, again, err)
		}
	})
}
