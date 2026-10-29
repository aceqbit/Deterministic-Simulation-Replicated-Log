package check

import (
	"math/rand/v2"
	"testing"
	"time"

	"github.com/aceqbit/seedlog/core"
	"github.com/aceqbit/seedlog/kv"
)

func put(c, call, ret int64, key uint32, v int64) Op {
	return Op{ClientID: uint64(c), Kind: kv.OpPut, Key: key, Value: v, Call: call, Return: ret, OK: true, Out: v}
}

func get(c, call, ret int64, key uint32, out int64) Op {
	return Op{ClientID: uint64(c), Kind: kv.OpGet, Key: key, Call: call, Return: ret, OK: true, Out: out}
}

func incr(c, call, ret int64, key uint32, d, out int64) Op {
	return Op{ClientID: uint64(c), Kind: kv.OpIncr, Key: key, Value: d, Call: call, Return: ret, OK: true, Out: out}
}

func TestHandWrittenHistories(t *testing.T) {
	tests := []struct {
		name string
		ops  []Op
		want LinResult
	}{
		{"sequential ok", []Op{put(1, 1, 2, 0, 5), get(1, 3, 4, 0, 5)}, LinOK},
		{"stale read", []Op{put(1, 1, 2, 0, 5), put(1, 3, 4, 0, 6), get(2, 5, 6, 0, 5)}, LinIllegal},
		{"lost write", []Op{put(1, 1, 2, 0, 5), get(2, 3, 4, 0, 0)}, LinIllegal},
		{"duplicate apply", []Op{incr(1, 1, 2, 0, 1, 1), incr(1, 3, 4, 0, 1, 3)}, LinIllegal},
		{"concurrent reads either value", []Op{put(1, 1, 10, 0, 5), get(2, 2, 3, 0, 0), get(3, 4, 5, 0, 5)}, LinOK},
		{"pending put may be read", []Op{{ClientID: 1, Kind: kv.OpPut, Key: 0, Value: 9, Call: 1, Return: Pending}, get(2, 2, 3, 0, 9)}, LinOK},
		{"keys independent", []Op{put(1, 1, 2, 0, 5), get(2, 3, 4, 1, 0)}, LinOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Linearizable(tt.ops, time.Second); got != tt.want {
				t.Errorf("porcupine = %v, want %v", got, tt.want)
			}
			if lin, ok := Oracle(tt.ops); ok && lin != (tt.want == LinOK) {
				t.Errorf("oracle = %v, want %v", lin, tt.want == LinOK)
			}
		})
	}
}

// randomHistory builds a history of n ops from a simulated register, with
// overlapping intervals, then optionally mutates one result.
func randomHistory(r *rand.Rand, n int, mutate bool) []Op {
	ops := make([]Op, n)
	state := map[uint32]int64{}
	clock := int64(0)
	for i := range ops {
		clock += int64(r.IntN(3))
		call := clock
		key := uint32(r.IntN(2))
		o := Op{ClientID: uint64(i%3 + 1), Key: key, Call: call}
		switch r.IntN(4) {
		case 0:
			o.Kind, o.Value = kv.OpPut, int64(r.IntN(4))
			state[key] = o.Value
			o.OK, o.Out = true, o.Value
		case 1:
			o.Kind, o.OK, o.Out = kv.OpGet, true, state[key]
		case 2:
			o.Kind, o.Expect, o.Value = kv.OpCAS, int64(r.IntN(4)), int64(r.IntN(4))
			if state[key] == o.Expect {
				state[key] = o.Value
				o.OK = true
			}
			o.Out = state[key]
		default:
			o.Kind, o.Value = kv.OpIncr, int64(1+r.IntN(2))
			state[key] += o.Value
			o.OK, o.Out = true, state[key]
		}
		o.Return = call + 1 + int64(r.IntN(6))
		ops[i] = o
	}
	if mutate {
		m := &ops[r.IntN(n)]
		m.Out += int64(1 + r.IntN(3))
		if m.Kind == kv.OpCAS {
			m.OK = !m.OK
		}
	}
	return ops
}

func TestOracleAgreesWithPorcupine(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	var illegal int
	for i := 0; i < 10000; i++ {
		ops := randomHistory(r, 3+r.IntN(6), i%2 == 1)
		want, ok := Oracle(ops)
		if !ok {
			t.Fatal("oracle refused a small history")
		}
		got := Linearizable(ops, 5*time.Second)
		if got == LinUnknown || (got == LinOK) != want {
			t.Fatalf("history %d: porcupine %v oracle %v: %+v", i, got, want, ops)
		}
		if !want {
			illegal++
		}
	}
	if illegal == 0 {
		t.Fatal("mutation never produced a non-linearizable history")
	}
}

func TestOracleTooLarge(t *testing.T) {
	if _, ok := Oracle(make([]Op, OracleMax+1)); ok {
		t.Fatal("oracle accepted an oversized history")
	}
}

func TestLiveness(t *testing.T) {
	h := &History{Ops: []Op{put(1, 1, 2, 0, 1)}}
	if v := Liveness(h, 10, 5, 100, true); v == nil || v.Invariant != "no_progress_after_heal" {
		t.Fatalf("want no_progress_after_heal, got %v", v)
	}
	h.Ops = append(h.Ops, put(1, 11, 12, 0, 2))
	if v := Liveness(h, 10, 5, 100, true); v != nil {
		t.Fatalf("unexpected %v", v)
	}
	if v := Liveness(h, 10, 5, 100, false); v == nil || v.Invariant != "no_leader_after_heal" {
		t.Fatalf("want no_leader_after_heal, got %v", v)
	}
	if v := Liveness(h, 10, 5, 12, false); v != nil {
		t.Fatalf("run ended before the budget; got %v", v)
	}
}

func obs(n int, k core.ObsKind, term, idx, a uint64) core.Observation {
	return core.Observation{Kind: k, Node: n, Term: term, Index: idx, A: a}
}

func TestOnlineInvariants(t *testing.T) {
	leader := func(o *Online, n int, term uint64) {
		o.Observe(0, obs(n, core.ObsState, term, 0, uint64(core.Leader)))
	}
	tests := []struct {
		name string
		run  func(o *Online)
		det  string
	}{
		{"two leaders", func(o *Online) { leader(o, 0, 1); leader(o, 1, 1) }, DetElectionSafety},
		{"leaders in different terms", func(o *Online) { leader(o, 0, 1); leader(o, 1, 2) }, ""},
		{"commit past end", func(o *Online) {
			o.Observe(0, obs(0, core.ObsAppend, 1, 1, 7))
			o.Observe(0, obs(0, core.ObsCommit, 0, 2, 0))
		}, DetCommitBound},
		{"leader truncates", func(o *Online) {
			o.Observe(0, obs(0, core.ObsAppend, 1, 1, 7))
			leader(o, 0, 1)
			o.Observe(0, obs(0, core.ObsTruncate, 0, 1, 0))
		}, DetLeaderAppendOnly},
		{"log matching", func(o *Online) {
			o.Observe(0, obs(0, core.ObsAppend, 1, 1, 7))
			o.Observe(0, obs(1, core.ObsAppend, 1, 1, 8))
		}, DetLogMatching},
		{"committed entry replaced", func(o *Online) {
			o.Observe(0, obs(0, core.ObsAppend, 1, 1, 7))
			o.Observe(0, obs(0, core.ObsCommit, 0, 1, 0))
			o.Observe(0, obs(1, core.ObsAppend, 2, 1, 9))
			o.Observe(0, obs(1, core.ObsCommit, 0, 1, 0))
		}, DetStateMachineSafety},
		{"leader lacks committed", func(o *Online) {
			o.Observe(0, obs(0, core.ObsAppend, 1, 1, 7))
			o.Observe(0, obs(0, core.ObsCommit, 0, 1, 0)) // committed in term 0
			leader(o, 1, 2)
		}, DetLeaderCompleteness},
		{"old-term commit by counting", func(o *Online) {
			o.Observe(0, obs(0, core.ObsAppend, 1, 1, 7))
			o.Observe(0, obs(0, core.ObsCommitOldTerm, 0, 1, 0))
		}, DetStateMachineSafety},
		{"late leader of older term is not bound by newer commits", func(o *Online) {
			o.Observe(0, obs(0, core.ObsState, 3, 0, uint64(core.Leader)))
			o.Observe(0, obs(0, core.ObsAppend, 1, 1, 7))
			o.Observe(0, obs(0, core.ObsCommit, 0, 1, 0)) // committed in term 3
			leader(o, 1, 2)
		}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := NewOnline(3)
			tt.run(o)
			v := o.Violation()
			switch {
			case tt.det == "" && v != nil:
				t.Fatalf("unexpected violation %v", v)
			case tt.det != "" && (v == nil || v.Detector != tt.det):
				t.Fatalf("want %s, got %v", tt.det, v)
			}
		})
	}
}

func TestLedger(t *testing.T) {
	o := NewOnline(2)
	o.Observe(0, obs(0, core.ObsAppend, 1, 1, 7))
	o.Observe(0, obs(0, core.ObsCommit, 0, 1, 0))
	var l Ledger
	l.Ack(1, 1, 1, 1, 7)
	if v := l.Check(o, 0); v != nil {
		t.Fatalf("held write reported lost: %v", v)
	}
	l.Reset()
	l.Ack(1, 2, 1, 1, 99)
	if v := l.Check(o, 0); v == nil || v.Detector != DetAckLedger {
		t.Fatalf("lost write not reported: %v", v)
	}
}

func TestTruncationOfCommittedEntry(t *testing.T) {
	setup := func() *Online {
		o := NewOnline(3)
		o.Observe(0, obs(2, core.ObsAppend, 1, 1, 7))
		o.Observe(0, obs(0, core.ObsAppend, 1, 1, 7))
		o.Observe(0, obs(0, core.ObsState, 2, 0, uint64(core.Leader)))
		o.Observe(0, obs(0, core.ObsCommit, 0, 1, 0)) // committed in term 2
		return o
	}
	trunc := func(o *Online, leaderTerm uint64) {
		o.Observe(0, obs(2, core.ObsTruncate, leaderTerm, 1, 0))
		o.EndStep(2)
	}
	if o := setup(); func() bool { trunc(o, 1); return o.Violation() != nil }() {
		t.Fatalf("delayed append from an earlier term wrongly flagged: %v", o.Violation())
	}
	o := setup()
	trunc(o, 2)
	if v := o.Violation(); v == nil || v.Invariant != "committed_entry_truncated" {
		t.Fatalf("truncation by a leader at the commit term not flagged: %v", v)
	}
}
