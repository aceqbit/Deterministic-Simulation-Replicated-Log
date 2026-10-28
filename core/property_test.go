package core

import (
	"testing"

	"github.com/aceqbit/seedlog/kv"
)

// xorshift is a tiny deterministic generator for property tests.
type xorshift uint64

func (x *xorshift) next(n uint64) uint64 {
	*x ^= *x << 13
	*x ^= *x >> 7
	*x ^= *x << 17
	return uint64(*x) % n
}

// TestPropertyRandomEvents feeds random event streams to one node and checks:
// the term never decreases, a granted vote leaves only after the batch holding
// it is durable, and log indices stay contiguous.
func TestPropertyRandomEvents(t *testing.T) {
	for seed := uint64(1); seed <= 200; seed++ {
		r := xorshift(seed * 0x9e3779b97f4a7c15)
		n := NewNode(testConfig(0, 3, Bugs{}), PersistentState{Vote: None})
		n.Step(Event{Kind: EvStart})
		var issued, acked uint64
		durableVote := map[uint64]int{} // term -> vote durably recorded
		pendingVote := map[uint64][2]uint64{}
		var lastTerm uint64
		for step := 0; step < 400; step++ {
			var ev Event
			term := r.next(6)
			switch r.next(7) {
			case 0:
				ev = Event{Kind: EvTimer, Timer: TimerKind(r.next(2))}
			case 1:
				ev = rv(int(1+r.next(2)), 0, term, r.next(5), r.next(5))
			case 2:
				ev = Event{Kind: EvMessage, Msg: Message{Type: MsgVoteReply, From: int(1 + r.next(2)), To: 0, Term: term, Granted: r.next(2) == 0}}
			case 3:
				prev := r.next(4)
				var ents []Entry
				for i := uint64(0); i < r.next(3); i++ {
					ents = append(ents, Entry{Term: term, Index: prev + 1 + i})
				}
				ev = appendMsg(term, prev, r.next(term+1), r.next(6), ents...)
				ev.Msg.From = int(1 + r.next(2))
			case 4:
				ev = Event{Kind: EvMessage, Msg: Message{Type: MsgAppendReply, From: int(1 + r.next(2)), To: 0, Term: term,
					Granted: r.next(2) == 0, Match: r.next(8), Hint: r.next(8)}}
			case 5:
				ev = Event{Kind: EvClient, Cmd: kv.Command{ClientID: 1, Seq: uint64(step), Op: kv.OpPut}}
			case 6:
				if issued > acked {
					acked = issued
					ev = Event{Kind: EvPersistDone, Token: acked}
					for tm, v := range pendingVote {
						if v[1] <= acked {
							durableVote[tm] = int(v[0])
							delete(pendingVote, tm)
						}
					}
				}
			}
			effs, _ := n.Step(ev)
			for _, e := range effs {
				switch e.Kind {
				case EffPersist:
					issued = e.Token
					if e.Batch.HardState && e.Batch.Vote != None {
						pendingVote[e.Batch.Term] = [2]uint64{uint64(e.Batch.Vote), e.Token}
					}
				case EffSend:
					if e.Msg.Type == MsgVoteReply && e.Msg.Granted {
						if v, ok := durableVote[e.Msg.Term]; !ok || v != e.Msg.To {
							t.Fatalf("seed %d step %d: vote for %d in term %d sent before durable", seed, step, e.Msg.To, e.Msg.Term)
						}
					}
				}
			}
			if n.Term() < lastTerm {
				t.Fatalf("seed %d: term decreased %d -> %d", seed, lastTerm, n.Term())
			}
			lastTerm = n.Term()
			for i := uint64(1); i <= n.LastIndex(); i++ {
				if e, _ := n.Entry(i); e.Index != i {
					t.Fatalf("seed %d: entry %d has index %d", seed, i, e.Index)
				}
			}
		}
	}
}
