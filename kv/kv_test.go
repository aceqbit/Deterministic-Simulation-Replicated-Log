package kv

import (
	"hash/fnv"
	"testing"
)

func TestApply(t *testing.T) {
	tests := []struct {
		name string
		cmds []Command
		want []Result
		dups []bool
	}{
		{"put get", []Command{{1, 1, OpPut, 3, 7, 0}, {1, 2, OpGet, 3, 0, 0}},
			[]Result{{true, 7}, {true, 7}}, []bool{false, false}},
		{"get absent", []Command{{1, 1, OpGet, 9, 0, 0}}, []Result{{true, 0}}, []bool{false}},
		{"cas hit", []Command{{1, 1, OpPut, 1, 5, 0}, {1, 2, OpCAS, 1, 6, 5}},
			[]Result{{true, 5}, {true, 6}}, []bool{false, false}},
		{"cas miss", []Command{{1, 1, OpPut, 1, 5, 0}, {1, 2, OpCAS, 1, 6, 4}},
			[]Result{{true, 5}, {false, 5}}, []bool{false, false}},
		{"incr", []Command{{1, 1, OpIncr, 2, 1, 0}, {1, 2, OpIncr, 2, 1, 0}},
			[]Result{{true, 1}, {true, 2}}, []bool{false, false}},
		{"retry applies once", []Command{{1, 1, OpIncr, 2, 1, 0}, {1, 1, OpIncr, 2, 1, 0}, {1, 2, OpGet, 2, 0, 0}},
			[]Result{{true, 1}, {true, 1}, {true, 1}}, []bool{false, true, false}},
		{"stale retry", []Command{{1, 1, OpIncr, 2, 1, 0}, {1, 2, OpIncr, 2, 1, 0}, {1, 1, OpIncr, 2, 1, 0}},
			[]Result{{true, 1}, {true, 2}, {}}, []bool{false, false, true}},
		{"noop", []Command{{0, 0, OpNoop, 0, 0, 0}}, []Result{{true, 0}}, []bool{false}},
		{"sessionless", []Command{{0, 0, OpIncr, 1, 1, 0}, {0, 0, OpIncr, 1, 1, 0}},
			[]Result{{true, 1}, {true, 2}}, []bool{false, false}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewStore()
			for i, c := range tt.cmds {
				got, dup := s.Apply(c, true)
				if got != tt.want[i] || dup != tt.dups[i] {
					t.Fatalf("cmd %d: got %+v dup=%v, want %+v dup=%v", i, got, dup, tt.want[i], tt.dups[i])
				}
			}
		})
	}
}

func TestDoubleApplyWithoutDedup(t *testing.T) {
	s := NewStore()
	c := Command{ClientID: 1, Seq: 1, Op: OpIncr, Key: 1, Value: 1}
	s.Apply(c, false)
	if got, dup := s.Apply(c, false); got.Value != 2 || dup {
		t.Fatalf("got %+v dup=%v", got, dup)
	}
	if s.Get(1) != 2 || s.Applied() != 2 || s.DedupHits() != 0 || s.LastSeq(1) != 1 {
		t.Fatal("counters")
	}
	s.Apply(c, true)
	if s.DedupHits() != 1 {
		t.Fatal("dedup hits")
	}
}

func TestCommandRoundTrip(t *testing.T) {
	c := Command{ClientID: 1 << 40, Seq: 99, Op: OpCAS, Key: 15, Value: -3, Expect: 1 << 50}
	b := c.AppendBinary(nil)
	if len(b) != CommandSize {
		t.Fatalf("size %d", len(b))
	}
	got, err := DecodeCommand(b)
	if err != nil || got != c {
		t.Fatalf("got %+v %v", got, err)
	}
	if _, err := DecodeCommand(b[:5]); err != ErrShortCommand {
		t.Fatal("short")
	}
	b[16] = 77
	if _, err := DecodeCommand(b); err != ErrBadOp {
		t.Fatal("bad op")
	}
	h := fnv.New64a()
	h.Write(c.AppendBinary(nil))
	if h.Sum64() != c.Hash() {
		t.Fatal("hash must equal hash/fnv FNV-1a over the encoding")
	}
}

func TestOpKind(t *testing.T) {
	names := map[OpKind]string{OpNoop: "noop", OpPut: "put", OpGet: "get", OpCAS: "cas", OpIncr: "incr", 9: "invalid"}
	for op, n := range names {
		if op.String() != n {
			t.Fatalf("%d: %s", op, op.String())
		}
	}
	if !OpPut.IsWrite() || OpGet.IsWrite() || OpNoop.IsWrite() {
		t.Fatal("IsWrite")
	}
}
