package core

import (
	"testing"

	"github.com/aceqbit/seedlog/kv"
)

func rv(from, to int, term, lastIndex, lastTerm uint64) Event {
	return Event{Kind: EvMessage, Msg: Message{Type: MsgRequestVote, From: from, To: to, Term: term, LastIndex: lastIndex, LastTerm: lastTerm}}
}

// sends returns the messages in effs, completing persistence first if gated.
func stepAll(n *Node, ev Event) ([]Effect, []Observation) {
	effs, obs := n.Step(ev)
	out := append([]Effect(nil), effs...)
	all := append([]Observation(nil), obs...)
	for _, e := range effs {
		if e.Kind == EffPersist {
			more, o := n.Step(Event{Kind: EvPersistDone, Token: e.Token})
			out = append(out, more...)
			all = append(all, o...)
		}
	}
	return out, all
}

func firstMsg(effs []Effect, t MsgType) (Message, bool) {
	for _, e := range effs {
		if e.Kind == EffSend && e.Msg.Type == t {
			return e.Msg, true
		}
	}
	return Message{}, false
}

func TestElection(t *testing.T) {
	tests := []struct {
		name  string
		run   func(c *cluster) bool
		check string
	}{
		{"single candidate wins", func(c *cluster) bool {
			c.elect(0)
			return len(c.leaders(1)) == 1 && c.leaders(1)[0] == 0
		}, "node 0 leads term 1"},
		{"split vote elects nobody", func(c *cluster) bool {
			// 4 nodes: 0 and 1 both campaign in term 1; 2 votes for 0, 3 for 1.
			c.step(0, Event{Kind: EvTimer, Timer: TimerElection})
			c.step(1, Event{Kind: EvTimer, Timer: TimerElection})
			c.persistAll()
			var to2from0, to3from1 []Message
			for _, m := range c.queue {
				if m.To == 2 && m.From == 0 {
					to2from0 = append(to2from0, m)
				}
				if m.To == 3 && m.From == 1 {
					to3from1 = append(to3from1, m)
				}
			}
			c.queue = nil
			for _, m := range append(to2from0, to3from1...) {
				c.step(m.To, Event{Kind: EvMessage, Msg: m})
			}
			c.settle()
			return len(c.leaders(1)) == 0 && c.nodes[0].Role() == Candidate && c.nodes[1].Role() == Candidate
		}, "no leader in term 1"},
		{"second election in higher term", func(c *cluster) bool {
			c.elect(0)
			c.elect(1)
			return len(c.leaders(2)) == 1 && c.leaders(2)[0] == 1 && c.nodes[0].Role() == Follower
		}, "node 1 leads term 2, node 0 stepped down"},
		{"heartbeat keeps leader", func(c *cluster) bool {
			c.elect(2)
			c.heartbeat(2)
			c.step(0, Event{Kind: EvTimer, Timer: TimerHeartbeat}) // ignored by follower
			c.step(2, Event{Kind: EvTimer, Timer: TimerElection})  // ignored by leader
			c.settle()
			return c.nodes[2].Role() == Leader && c.nodes[0].Leader() == 2
		}, "node 2 still leads"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := 5
			if tt.name == "split vote elects nobody" {
				n = 4
			}
			if !tt.run(newCluster(n, Bugs{})) {
				t.Fatal(tt.check)
			}
		})
	}
}

func TestVoteRules(t *testing.T) {
	logged := PersistentState{Term: 2, Vote: None, Entries: []Entry{{Term: 1, Index: 1}, {Term: 2, Index: 2}}}
	tests := []struct {
		name    string
		state   PersistentState
		bugs    Bugs
		events  []Event
		granted []bool
		term    uint64
	}{
		{"higher term granted", PersistentState{Vote: None}, Bugs{}, []Event{rv(1, 0, 3, 0, 0)}, []bool{true}, 3},
		{"stale term rejected", PersistentState{Term: 5, Vote: None}, Bugs{}, []Event{rv(1, 0, 4, 0, 0)}, []bool{false}, 5},
		{"one vote per term", PersistentState{Vote: None}, Bugs{}, []Event{rv(1, 0, 1, 0, 0), rv(2, 0, 1, 0, 0)}, []bool{true, false}, 1},
		{"repeat vote same candidate", PersistentState{Vote: None}, Bugs{}, []Event{rv(1, 0, 1, 0, 0), rv(1, 0, 1, 0, 0)}, []bool{true, true}, 1},
		{"shorter log rejected", logged, Bugs{}, []Event{rv(1, 0, 3, 1, 1)}, []bool{false}, 3},
		{"older last term rejected", logged, Bugs{}, []Event{rv(1, 0, 3, 5, 1)}, []bool{false}, 3},
		{"newer last term granted", logged, Bugs{}, []Event{rv(1, 0, 3, 1, 3)}, []bool{true}, 3},
		{"bug 2 skips check", logged, Bugs{SkipUptodateCheck: true}, []Event{rv(1, 0, 3, 1, 1)}, []bool{true}, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := NewNode(testConfig(0, 3, tt.bugs), tt.state)
			n.Step(Event{Kind: EvStart})
			for i, ev := range tt.events {
				effs, _ := stepAll(n, ev)
				m, ok := firstMsg(effs, MsgVoteReply)
				if !ok || m.Granted != tt.granted[i] {
					t.Fatalf("event %d: reply %+v ok=%v, want granted=%v", i, m, ok, tt.granted[i])
				}
			}
			if n.Term() != tt.term {
				t.Fatalf("term %d want %d", n.Term(), tt.term)
			}
		})
	}
}

func TestVoteGatedOnPersist(t *testing.T) {
	for _, bug := range []bool{false, true} {
		n := NewNode(testConfig(0, 3, Bugs{VoteBeforePersist: bug}), PersistentState{Vote: None})
		n.Step(Event{Kind: EvStart})
		effs, obs := n.Step(rv(1, 0, 1, 0, 0))
		_, sent := firstMsg(effs, MsgVoteReply)
		if sent != bug {
			t.Fatalf("bug=%v: reply sent before persist=%v", bug, sent)
		}
		var token uint64
		for _, e := range effs {
			if e.Kind == EffPersist {
				token = e.Token
				if !e.Batch.HardState || e.Batch.Vote != 1 || e.Batch.Term != 1 {
					t.Fatalf("batch %+v", e.Batch)
				}
			}
		}
		if !bug && !hasObs(obs, ObsGated) {
			t.Fatal("gating not observed")
		}
		effs, _ = n.Step(Event{Kind: EvPersistDone, Token: token})
		if _, sent := firstMsg(effs, MsgVoteReply); sent == bug {
			t.Fatalf("bug=%v: release after persist", bug)
		}
		// Stale and future tokens are ignored.
		if effs, _ := n.Step(Event{Kind: EvPersistDone, Token: token}); len(effs) != 0 {
			t.Fatal("stale token released effects")
		}
		if effs, _ := n.Step(Event{Kind: EvPersistDone, Token: token + 5}); len(effs) != 0 {
			t.Fatal("future token released effects")
		}
	}
}

func hasObs(obs []Observation, k ObsKind) bool {
	for _, o := range obs {
		if o.Kind == k {
			return true
		}
	}
	return false
}

func put(client, seq uint64, key uint32, v int64) kv.Command {
	return kv.Command{ClientID: client, Seq: seq, Op: kv.OpPut, Key: key, Value: v}
}

func TestReplicationAndApply(t *testing.T) {
	c := newCluster(5, Bugs{})
	c.elect(0)
	for s := uint64(1); s <= 10; s++ {
		c.client(0, put(1, s, uint32(s%3), int64(s)))
	}
	c.client(0, kv.Command{ClientID: 1, Seq: 11, Op: kv.OpIncr, Key: 0, Value: 5})
	c.client(0, kv.Command{ClientID: 1, Seq: 12, Op: kv.OpCAS, Key: 0, Expect: 14, Value: 100})
	c.client(0, kv.Command{ClientID: 1, Seq: 13, Op: kv.OpGet, Key: 0})
	c.heartbeat(0)
	for i, n := range c.nodes {
		if n.Commit() != 14 || n.Applied() != 14 || n.LastIndex() != 14 {
			t.Fatalf("node %d commit=%d applied=%d last=%d", i, n.Commit(), n.Applied(), n.LastIndex())
		}
		if n.Store().Get(0) != 100 {
			t.Fatalf("node %d value %d", i, n.Store().Get(0))
		}
	}
	if len(c.replies) != 13 {
		t.Fatalf("replies %d", len(c.replies))
	}
	last := c.replies[len(c.replies)-1]
	if last.Status != ReplyOK || last.Result.Value != 100 || last.Index != 14 || last.Term != 1 {
		t.Fatalf("last reply %+v", last)
	}
	// Followers redirect.
	c.step(3, Event{Kind: EvClient, Cmd: put(2, 1, 1, 1)})
	if r := c.replies[len(c.replies)-1]; r.Status != ReplyNotLeader || r.Leader != 0 {
		t.Fatalf("redirect %+v", r)
	}
}

func TestRetryAppliesOnce(t *testing.T) {
	for _, bug := range []bool{false, true} {
		c := newCluster(3, Bugs{DoubleApplyRetry: bug})
		c.elect(0)
		incr := kv.Command{ClientID: 1, Seq: 1, Op: kv.OpIncr, Key: 7, Value: 1}
		c.client(0, incr)
		c.client(0, incr)
		want := int64(1)
		if bug {
			want = 2
		}
		if got := c.nodes[0].Store().Get(7); got != want {
			t.Fatalf("bug=%v value %d want %d", bug, got, want)
		}
	}
}

func TestFollowerCatchUpAndConflict(t *testing.T) {
	c := newCluster(3, Bugs{})
	c.elect(0)
	c.down[2] = true
	for s := uint64(1); s <= 9; s++ {
		c.client(0, put(1, s, 1, int64(s)))
	}
	c.queue = nil
	c.down[2] = false
	c.heartbeat(0) // node 2 rejects, leader backs up with batches of MaxBatch
	if c.nodes[2].LastIndex() != c.nodes[0].LastIndex() {
		t.Fatalf("follower last %d leader %d", c.nodes[2].LastIndex(), c.nodes[0].LastIndex())
	}
	// Node 1 goes away; node 0 appends entries only it has; node 2 becomes
	// leader in term 2 with node 1, and node 0's extra entries are truncated.
	c.down[1] = true
	c.step(0, Event{Kind: EvClient, Cmd: put(1, 10, 1, 10)})
	c.step(0, Event{Kind: EvClient, Cmd: put(1, 11, 1, 11)})
	c.queue = nil
	c.down[1] = false
	c.down[0] = true
	c.elect(2)
	c.client(2, put(2, 1, 1, 50))
	c.down[0] = false
	c.heartbeat(2)
	c.heartbeat(2)
	n0, n2 := c.nodes[0], c.nodes[2]
	if n0.Role() != Follower || n0.LastIndex() != n2.LastIndex() || n0.Store().Get(1) != 50 {
		t.Fatalf("node0 role=%v last=%d/%d value=%d", n0.Role(), n0.LastIndex(), n2.LastIndex(), n0.Store().Get(1))
	}
	for i := uint64(1); i <= n2.LastIndex(); i++ {
		a, _ := n0.Entry(i)
		b, _ := n2.Entry(i)
		if a != b {
			t.Fatalf("index %d differs: %+v %+v", i, a, b)
		}
	}
	if _, ok := n0.Entry(0); ok {
		t.Fatal("entry 0 must not exist")
	}
}

func TestCommitRuleCurrentTermOnly(t *testing.T) {
	old := PersistentState{Term: 2, Vote: None, Entries: []Entry{{Term: 1, Index: 1}, {Term: 1, Index: 2}}}
	for _, bug := range []bool{false, true} {
		n := NewNode(testConfig(0, 5, Bugs{Fig8Commit: bug}), old)
		n.Step(Event{Kind: EvStart})
		stepAll(n, Event{Kind: EvTimer, Timer: TimerElection})
		for p := 1; p <= 2; p++ {
			stepAll(n, Event{Kind: EvMessage, Msg: Message{Type: MsgVoteReply, From: p, To: 0, Term: 3, Granted: true}})
		}
		if n.Role() != Leader {
			t.Fatal("not leader")
		}
		for p := 1; p <= 2; p++ {
			stepAll(n, Event{Kind: EvMessage, Msg: Message{Type: MsgAppendReply, From: p, To: 0, Term: 3, Granted: true, Match: 2}})
		}
		want := uint64(0)
		if bug {
			want = 2
		}
		if n.Commit() != want {
			t.Fatalf("bug=%v commit %d want %d", bug, n.Commit(), want)
		}
	}
}

func TestStepDownAndNoStepdownBug(t *testing.T) {
	for _, bug := range []bool{false, true} {
		c := newCluster(3, Bugs{NoStepdown: bug})
		c.elect(0)
		c.step(0, Event{Kind: EvMessage, Msg: Message{Type: MsgAppendReply, From: 1, To: 0, Term: 7}})
		n := c.nodes[0]
		if n.Term() != 7 {
			t.Fatalf("term %d", n.Term())
		}
		if (n.Role() == Leader) != bug {
			t.Fatalf("bug=%v role %v", bug, n.Role())
		}
		if bug {
			c.persistAll()
			effs, _ := stepAll(n, Event{Kind: EvMessage, Msg: Message{Type: MsgAppend, From: 1, To: 0, Term: 7}})
			if m, ok := firstMsg(effs, MsgAppendReply); !ok || m.Granted {
				t.Fatal("leader must reject same-term append")
			}
		}
	}
}

func TestStaleMessages(t *testing.T) {
	n := NewNode(testConfig(0, 3, Bugs{}), PersistentState{Term: 4, Vote: None})
	n.Step(Event{Kind: EvStart})
	effs, obs := stepAll(n, Event{Kind: EvMessage, Msg: Message{Type: MsgAppend, From: 1, To: 0, Term: 3}})
	if m, ok := firstMsg(effs, MsgAppendReply); !ok || m.Granted || m.Term != 4 || !hasObs(obs, ObsStaleTerm) {
		t.Fatalf("stale append reply %+v", m)
	}
	effs, _ = stepAll(n, Event{Kind: EvMessage, Msg: Message{Type: MsgVoteReply, From: 1, To: 0, Term: 3, Granted: true}})
	if len(effs) != 0 {
		t.Fatal("stale vote reply produced effects")
	}
	// A vote reply to a follower is ignored.
	effs, _ = stepAll(n, Event{Kind: EvMessage, Msg: Message{Type: MsgVoteReply, From: 1, To: 0, Term: 4, Granted: true}})
	if len(effs) != 0 || n.Role() != Follower {
		t.Fatal("follower counted a vote")
	}
	// An append reply to a follower is ignored.
	stepAll(n, Event{Kind: EvMessage, Msg: Message{Type: MsgAppendReply, From: 1, To: 0, Term: 4, Granted: true, Match: 9}})
	if n.Commit() != 0 {
		t.Fatal("follower committed from reply")
	}
}

func appendMsg(term, prevIndex, prevTerm, commit uint64, ents ...Entry) Event {
	return Event{Kind: EvMessage, Msg: Message{Type: MsgAppend, From: 1, To: 0, Term: term,
		PrevIndex: prevIndex, PrevTerm: prevTerm, Commit: commit, Entries: ents}}
}

func TestAppendRules(t *testing.T) {
	e := func(term, idx uint64) Entry { return Entry{Term: term, Index: idx, Cmd: put(1, idx, 0, int64(idx))} }
	tests := []struct {
		name    string
		bugs    Bugs
		events  []Event
		success bool
		last    uint64
		commit  uint64
		hint    uint64
	}{
		{"append to empty", Bugs{}, []Event{appendMsg(1, 0, 0, 1, e(1, 1), e(1, 2))}, true, 2, 1, 0},
		{"missing prev", Bugs{}, []Event{appendMsg(1, 5, 1, 0, e(1, 6))}, false, 0, 0, 1},
		{"term mismatch hint", Bugs{}, []Event{appendMsg(1, 0, 0, 0, e(1, 1), e(1, 2), e(1, 3)), appendMsg(2, 3, 2, 0)}, false, 3, 0, 1},
		{"duplicate is idempotent", Bugs{}, []Event{appendMsg(1, 0, 0, 0, e(1, 1), e(1, 2)), appendMsg(1, 0, 0, 0, e(1, 1))}, true, 2, 0, 0},
		{"conflict truncates", Bugs{}, []Event{appendMsg(1, 0, 0, 0, e(1, 1), e(1, 2)), appendMsg(2, 1, 1, 0, e(2, 2))}, true, 2, 0, 0},
		{"commit bounded by match", Bugs{}, []Event{appendMsg(1, 0, 0, 0, e(1, 1), e(1, 2)), appendMsg(1, 0, 0, 9)}, true, 2, 0, 0},
		{"bug 6 commit past end", Bugs{CommitPastEnd: true}, []Event{appendMsg(1, 0, 0, 9, e(1, 1))}, true, 1, 9, 0},
		{"bug 4 stale append truncates", Bugs{TruncateAlways: true}, []Event{appendMsg(1, 0, 0, 0, e(1, 1), e(1, 2)), appendMsg(1, 0, 0, 0, e(1, 1))}, true, 1, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := NewNode(testConfig(0, 3, tt.bugs), PersistentState{Vote: None})
			n.Step(Event{Kind: EvStart})
			var reply Message
			for _, ev := range tt.events {
				effs, _ := stepAll(n, ev)
				reply, _ = firstMsg(effs, MsgAppendReply)
			}
			if reply.Granted != tt.success || n.LastIndex() != tt.last || n.Commit() != tt.commit || reply.Hint != tt.hint {
				t.Fatalf("success=%v last=%d commit=%d hint=%d", reply.Granted, n.LastIndex(), n.Commit(), reply.Hint)
			}
		})
	}
}

func TestCandidateStepsDownOnAppend(t *testing.T) {
	n := NewNode(testConfig(0, 3, Bugs{}), PersistentState{Vote: None})
	n.Step(Event{Kind: EvStart})
	stepAll(n, Event{Kind: EvTimer, Timer: TimerElection})
	if n.Role() != Candidate {
		t.Fatal("not candidate")
	}
	stepAll(n, appendMsg(1, 0, 0, 0))
	if n.Role() != Follower || n.Leader() != 1 || n.Vote() != 0 {
		t.Fatalf("role %v leader %d vote %d", n.Role(), n.Leader(), n.Vote())
	}
}

func TestSingleNodeCluster(t *testing.T) {
	n := NewNode(testConfig(0, 1, Bugs{}), PersistentState{Vote: None})
	n.Step(Event{Kind: EvStart})
	stepAll(n, Event{Kind: EvTimer, Timer: TimerElection})
	stepAll(n, Event{Kind: EvClient, Cmd: put(1, 1, 1, 3)})
	if n.Role() != Leader || n.Commit() != 2 || n.Store().Get(1) != 3 || n.ID() != 0 {
		t.Fatalf("role %v commit %d", n.Role(), n.Commit())
	}
}

func TestStaleLeaderReadBug(t *testing.T) {
	for _, bug := range []bool{false, true} {
		c := newCluster(3, Bugs{StaleLeaderRead: bug})
		c.elect(0)
		last := c.nodes[0].LastIndex()
		c.step(0, Event{Kind: EvClient, Cmd: kv.Command{ClientID: 1, Seq: 1, Op: kv.OpGet, Key: 1}})
		local := c.nodes[0].LastIndex() == last
		if local != bug {
			t.Fatalf("bug=%v local read=%v", bug, local)
		}
	}
}

func TestAckBeforeFsyncBug(t *testing.T) {
	for _, bug := range []bool{false, true} {
		n := NewNode(testConfig(0, 3, Bugs{AckBeforeFsync: bug}), PersistentState{Vote: None})
		n.Step(Event{Kind: EvStart})
		effs, _ := n.Step(appendMsg(1, 0, 0, 0, Entry{Term: 1, Index: 1}))
		if _, sent := firstMsg(effs, MsgAppendReply); sent != bug {
			t.Fatalf("bug=%v ack before fsync=%v", bug, sent)
		}
	}
}

func TestRestartRecoversState(t *testing.T) {
	c := newCluster(3, Bugs{})
	c.elect(0)
	c.client(0, put(1, 1, 2, 9))
	c.restart(1)
	n := c.nodes[1]
	if n.Term() != 1 || n.Vote() != 0 || n.LastIndex() != 2 || n.Commit() != 0 {
		t.Fatalf("term %d vote %d last %d commit %d", n.Term(), n.Vote(), n.LastIndex(), n.Commit())
	}
	c.heartbeat(0)
	if n.Store().Get(2) != 9 {
		t.Fatal("restarted node did not re-apply")
	}
}

func TestBugByNumber(t *testing.T) {
	for i := 0; i <= 10; i++ {
		b, err := BugByNumber(i)
		if err != nil {
			t.Fatal(err)
		}
		set := 0
		for _, v := range []bool{b.VoteBeforePersist, b.SkipUptodateCheck, b.Fig8Commit, b.TruncateAlways, b.NoStepdown,
			b.CommitPastEnd, b.StaleLeaderRead, b.AckBeforeFsync, b.DoubleApplyRetry, b.AcceptTornTail} {
			if v {
				set++
			}
		}
		if (i == 0 && set != 0) || (i > 0 && set != 1) {
			t.Fatalf("bug %d sets %d flags", i, set)
		}
	}
	if _, err := BugByNumber(11); err != ErrBug {
		t.Fatal("bug 11")
	}
	for r, s := range map[Role]string{Follower: "follower", Candidate: "candidate", Leader: "leader", 9: "invalid"} {
		if r.String() != s {
			t.Fatal(s)
		}
	}
}
