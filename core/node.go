package core

import "github.com/aceqbit/seedlog/kv"

// PersistentState is what a node recovers from its WAL.
type PersistentState struct {
	Term    uint64
	Vote    int
	Entries []Entry
}

type mark struct {
	token uint64
	last  uint64
}

type held struct {
	token uint64
	eff   Effect
}

// Node is one Raft participant. It is not safe for concurrent use; the slices
// returned by Step are reused by the next call.
type Node struct {
	cfg      Config
	majority int

	role   Role
	term   uint64
	vote   int
	leader int

	log     []Entry // log[0] is a sentinel with Index 0, Term 0
	commit  uint64
	applied uint64
	store   *kv.Store

	votes    []bool
	next     []uint64
	match    []uint64
	inflight []bool

	// Durability: every Step that changes durable state emits Persist with a
	// new token; outputs produced while a token is outstanding are held.
	token   uint64
	done    uint64
	durable uint64
	marks   []mark
	hold    []held
	batch   PersistBatch
	hard    bool

	out     []Effect // gated outputs of the current Step
	effects []Effect
	obs     []Observation
}

// NewNode returns a node restored from st. It does nothing until it receives
// EvStart.
func NewNode(cfg Config, st PersistentState) *Node {
	if cfg.MaxBatch <= 0 {
		cfg.MaxBatch = 64
	}
	n := &Node{
		cfg:      cfg,
		majority: cfg.N/2 + 1,
		term:     st.Term,
		vote:     st.Vote,
		leader:   None,
		store:    kv.NewStore(),
		votes:    make([]bool, cfg.N),
		next:     make([]uint64, cfg.N),
		match:    make([]uint64, cfg.N),
		inflight: make([]bool, cfg.N),
	}
	n.log = make([]Entry, 1, len(st.Entries)+1)
	n.log = append(n.log, st.Entries...)
	n.durable = n.last()
	return n
}

// ID returns the node ID.
func (n *Node) ID() int { return n.cfg.ID }

// Role returns the current role.
func (n *Node) Role() Role { return n.role }

// Term returns the current term.
func (n *Node) Term() uint64 { return n.term }

// Vote returns the candidate voted for in the current term, or None.
func (n *Node) Vote() int { return n.vote }

// Leader returns the known leader, or None.
func (n *Node) Leader() int { return n.leader }

// Commit returns the commit index.
func (n *Node) Commit() uint64 { return n.commit }

// Applied returns the last applied index.
func (n *Node) Applied() uint64 { return n.applied }

// LastIndex returns the index of the last log entry.
func (n *Node) LastIndex() uint64 { return n.last() }

// Entry returns the entry at index i and whether it exists.
func (n *Node) Entry(i uint64) (Entry, bool) {
	if i == 0 || i > n.last() {
		return Entry{}, false
	}
	return n.log[i], true
}

// Store returns the node's state machine.
func (n *Node) Store() *kv.Store { return n.store }

// Step consumes one event.
func (n *Node) Step(ev Event) ([]Effect, []Observation) {
	n.effects = n.effects[:0]
	n.obs = n.obs[:0]
	n.out = n.out[:0]
	switch ev.Kind {
	case EvStart:
		n.start()
	case EvTimer:
		n.onTimer(ev.Timer)
	case EvMessage:
		n.onMessage(&ev.Msg)
	case EvPersistDone:
		n.onPersistDone(ev.Token)
	case EvClient:
		n.onClient(ev.Cmd)
	}
	n.flush()
	return n.effects, n.obs
}

func (n *Node) last() uint64 { return uint64(len(n.log) - 1) }

func (n *Node) termAt(i uint64) uint64 {
	if i > n.last() {
		return 0
	}
	return n.log[i].Term
}

func (n *Node) emit(o Observation) {
	o.Node = n.cfg.ID
	n.obs = append(n.obs, o)
}

func (n *Node) observeState() {
	n.emit(Observation{Kind: ObsState, Term: n.term, A: uint64(n.role)})
}

func (n *Node) start() {
	n.emit(Observation{Kind: ObsRestore, Index: n.last(), Term: n.term})
	for _, e := range n.log[1:] {
		n.emit(Observation{Kind: ObsAppend, Index: e.Index, Term: e.Term, A: e.Cmd.Hash()})
	}
	n.observeState()
	n.resetElection()
}

func (n *Node) resetElection() {
	n.effects = append(n.effects, Effect{Kind: EffSetTimer, Timer: TimerElection,
		Base: n.cfg.ElectionBase, Jitter: n.cfg.ElectionJitter})
}

func (n *Node) resetHeartbeat() {
	n.effects = append(n.effects, Effect{Kind: EffSetTimer, Timer: TimerHeartbeat, Base: n.cfg.Heartbeat})
}

// send queues m behind the current persistence token, or immediately when
// gate is false.
func (n *Node) send(m Message, gate bool) {
	m.From = n.cfg.ID
	e := Effect{Kind: EffSend, Msg: m}
	if gate {
		n.out = append(n.out, e)
	} else {
		n.effects = append(n.effects, e)
	}
}

func (n *Node) reply(r Reply, gate bool) {
	e := Effect{Kind: EffReply, Reply: r}
	if gate {
		n.out = append(n.out, e)
	} else {
		n.effects = append(n.effects, e)
	}
}

func (n *Node) setHard() { n.hard = true }

func (n *Node) onTimer(k TimerKind) {
	switch k {
	case TimerElection:
		if n.role != Leader {
			n.startElection()
		}
	case TimerHeartbeat:
		if n.role == Leader {
			n.emit(Observation{Kind: ObsHeartbeat, Term: n.term})
			n.broadcast(true)
			n.resetHeartbeat()
		}
	}
}

func (n *Node) startElection() {
	n.term++
	n.vote = n.cfg.ID
	n.role = Candidate
	n.leader = None
	n.setHard()
	for i := range n.votes {
		n.votes[i] = false
	}
	n.votes[n.cfg.ID] = true
	n.emit(Observation{Kind: ObsElectionStart, Term: n.term})
	n.observeState()
	n.resetElection()
	if n.countVotes() >= n.majority {
		n.becomeLeader()
		return
	}
	for p := 0; p < n.cfg.N; p++ {
		if p != n.cfg.ID {
			n.send(Message{Type: MsgRequestVote, To: p, Term: n.term,
				LastIndex: n.last(), LastTerm: n.termAt(n.last())}, true)
		}
	}
}

func (n *Node) countVotes() int {
	c := 0
	for _, v := range n.votes {
		if v {
			c++
		}
	}
	return c
}

func (n *Node) becomeFollower(term uint64, leader int) {
	if n.role != Follower {
		n.emit(Observation{Kind: ObsStepDown, Term: term, A: uint64(n.role)})
		n.role = Follower
		n.resetElection()
	}
	if term > n.term {
		n.term = term
		n.vote = None
		n.setHard()
	}
	n.leader = leader
	n.observeState()
}

func (n *Node) becomeLeader() {
	n.role = Leader
	n.leader = n.cfg.ID
	n.observeState()
	for p := range n.next {
		n.next[p] = n.last() + 1
		n.match[p] = 0
		n.inflight[p] = false
	}
	n.appendLocal(kv.Command{Op: kv.OpNoop})
	n.updateSelfMatch()
	n.maybeCommit()
	n.broadcast(true)
	n.resetHeartbeat()
}

func (n *Node) appendLocal(cmd kv.Command) {
	e := Entry{Term: n.term, Index: n.last() + 1, Cmd: cmd}
	n.log = append(n.log, e)
	n.batch.Entries = append(n.batch.Entries, e)
	n.emit(Observation{Kind: ObsAppend, Index: e.Index, Term: e.Term, A: cmd.Hash()})
}

// truncate removes every entry with Index >= from.
func (n *Node) truncate(from uint64) {
	if from > n.last() {
		return
	}
	n.log = n.log[:from]
	if !n.batch.Truncate || from < n.batch.TruncateFrom {
		n.batch.Truncate = true
		n.batch.TruncateFrom = from
	}
	k := 0
	for _, e := range n.batch.Entries {
		if e.Index < from {
			n.batch.Entries[k] = e
			k++
		}
	}
	n.batch.Entries = n.batch.Entries[:k]
	for i := range n.marks {
		n.marks[i].last = min(n.marks[i].last, from-1)
	}
	n.durable = min(n.durable, from-1)
	n.emit(Observation{Kind: ObsTruncate, Index: from, Term: n.term})
}

func (n *Node) onMessage(m *Message) {
	if m.Term > n.term {
		if n.role == Leader && n.cfg.Bugs.NoStepdown {
			n.term = m.Term
			n.vote = None
			n.setHard()
			n.observeState()
		} else {
			lead := None
			if m.Type == MsgAppend {
				lead = m.From
			}
			n.becomeFollower(m.Term, lead)
		}
	}
	if m.Term < n.term {
		n.emit(Observation{Kind: ObsStaleTerm, Term: m.Term})
		switch m.Type {
		case MsgRequestVote:
			n.send(Message{Type: MsgVoteReply, To: m.From, Term: n.term}, true)
		case MsgAppend:
			n.send(Message{Type: MsgAppendReply, To: m.From, Term: n.term}, true)
		}
		return
	}
	switch m.Type {
	case MsgRequestVote:
		n.onRequestVote(m)
	case MsgVoteReply:
		n.onVoteReply(m)
	case MsgAppend:
		n.onAppend(m)
	case MsgAppendReply:
		n.onAppendReply(m)
	}
}

func (n *Node) onRequestVote(m *Message) {
	lastTerm := n.termAt(n.last())
	upToDate := m.LastTerm > lastTerm || (m.LastTerm == lastTerm && m.LastIndex >= n.last())
	if n.cfg.Bugs.SkipUptodateCheck {
		upToDate = true
	}
	canVote := n.vote == None || n.vote == m.From
	grant := canVote && upToDate
	switch {
	case grant:
		n.vote = m.From
		n.setHard()
		n.resetElection()
		n.emit(Observation{Kind: ObsVoteGranted, Term: n.term, A: uint64(m.From)})
	case !canVote:
		n.emit(Observation{Kind: ObsVoteRejectedVoted, Term: n.term, A: uint64(m.From)})
	default:
		n.emit(Observation{Kind: ObsVoteRejectedLog, Term: n.term, A: uint64(m.From)})
	}
	n.emit(Observation{Kind: ObsRPC, A: uint64(MsgRequestVote), B: rpcResult(grant, false)})
	gate := !grant || !n.cfg.Bugs.VoteBeforePersist
	n.send(Message{Type: MsgVoteReply, To: m.From, Term: n.term, Granted: grant}, gate)
}

func rpcResult(ok, conflict bool) uint64 {
	switch {
	case ok:
		return 0
	case conflict:
		return 2
	}
	return 1
}

func (n *Node) onVoteReply(m *Message) {
	if n.role != Candidate || !m.Granted {
		return
	}
	n.votes[m.From] = true
	if n.countVotes() >= n.majority {
		n.becomeLeader()
	}
}

func (n *Node) onAppend(m *Message) {
	if n.role == Leader {
		// Two leaders in one term; only reachable with bug no_stepdown.
		n.send(Message{Type: MsgAppendReply, To: m.From, Term: n.term}, true)
		return
	}
	if n.role == Candidate {
		n.becomeFollower(n.term, m.From)
	}
	n.leader = m.From
	n.resetElection()
	if m.PrevIndex > n.last() {
		n.emit(Observation{Kind: ObsAppendReject, Index: m.PrevIndex, A: 1})
		n.emit(Observation{Kind: ObsRPC, A: uint64(MsgAppend), B: rpcResult(false, false)})
		n.send(Message{Type: MsgAppendReply, To: m.From, Term: n.term, Hint: n.last() + 1}, true)
		return
	}
	if t := n.termAt(m.PrevIndex); t != m.PrevTerm {
		hint := m.PrevIndex
		for hint > n.commit+1 && n.termAt(hint-1) == t {
			hint--
		}
		n.emit(Observation{Kind: ObsAppendReject, Index: m.PrevIndex, A: 2})
		n.emit(Observation{Kind: ObsRPC, A: uint64(MsgAppend), B: rpcResult(false, true)})
		n.send(Message{Type: MsgAppendReply, To: m.From, Term: n.term, Hint: max(hint, 1)}, true)
		return
	}
	if n.cfg.Bugs.TruncateAlways && len(m.Entries) > 0 {
		n.truncate(m.PrevIndex + 1)
	}
	for _, e := range m.Entries {
		if e.Index <= n.last() {
			if n.log[e.Index].Term == e.Term {
				continue
			}
			n.truncate(e.Index)
		}
		n.log = append(n.log, e)
		n.batch.Entries = append(n.batch.Entries, e)
		n.emit(Observation{Kind: ObsAppend, Index: e.Index, Term: e.Term, A: e.Cmd.Hash()})
	}
	if len(m.Entries) > 1 {
		n.emit(Observation{Kind: ObsBatch, Index: uint64(len(m.Entries))})
	}
	match := m.PrevIndex + uint64(len(m.Entries))
	c := min(m.Commit, match)
	if n.cfg.Bugs.CommitPastEnd {
		c = m.Commit
	}
	if c > n.commit {
		n.commit = c
		n.emit(Observation{Kind: ObsCommit, Index: c})
		n.apply()
	}
	n.emit(Observation{Kind: ObsRPC, A: uint64(MsgAppend), B: rpcResult(true, false)})
	n.send(Message{Type: MsgAppendReply, To: m.From, Term: n.term, Granted: true, Match: match},
		!n.cfg.Bugs.AckBeforeFsync)
}

func (n *Node) onAppendReply(m *Message) {
	if n.role != Leader {
		return
	}
	p := m.From
	n.inflight[p] = false
	if m.Granted {
		progress := m.Match > n.match[p]
		if progress {
			n.match[p] = m.Match
		}
		n.next[p] = max(n.next[p], n.match[p]+1)
		n.maybeCommit()
		if !progress {
			// A duplicate or reordered ack must not start a second send
			// pipeline; the next heartbeat or client write resumes sending.
			return
		}
	} else {
		nx := n.next[p] - 1
		if m.Hint > 0 && m.Hint < n.next[p] {
			nx = m.Hint
		}
		n.next[p] = max(nx, n.match[p]+1, 1)
	}
	if n.role == Leader && n.next[p] <= n.last() {
		n.sendAppend(p)
	}
}

func (n *Node) sendAppend(p int) {
	nx := n.next[p]
	end := min(n.last(), nx+uint64(n.cfg.MaxBatch)-1)
	var ents []Entry
	if nx <= end {
		ents = make([]Entry, end-nx+1)
		copy(ents, n.log[nx:end+1])
		n.inflight[p] = true
	}
	n.send(Message{Type: MsgAppend, To: p, Term: n.term, PrevIndex: nx - 1,
		PrevTerm: n.termAt(nx - 1), Entries: ents, Commit: n.commit}, true)
}

// broadcast sends AppendEntries to every peer; heartbeat includes peers that
// already have a request in flight.
func (n *Node) broadcast(heartbeat bool) {
	for p := 0; p < n.cfg.N; p++ {
		if p == n.cfg.ID {
			continue
		}
		if heartbeat || (!n.inflight[p] && n.next[p] <= n.last()) {
			n.sendAppend(p)
		}
	}
}

func (n *Node) updateSelfMatch() {
	self := n.durable
	if n.cfg.Bugs.AckBeforeFsync {
		self = n.last()
	}
	n.match[n.cfg.ID] = min(self, n.last())
}

func (n *Node) maybeCommit() {
	if n.role != Leader {
		return
	}
	n.updateSelfMatch()
	for i := n.last(); i > n.commit; i-- {
		t := n.log[i].Term
		if t != n.term && !n.cfg.Bugs.Fig8Commit {
			break
		}
		c := 0
		for _, m := range n.match {
			if m >= i {
				c++
			}
		}
		if c >= n.majority {
			n.commit = i
			n.emit(Observation{Kind: ObsCommit, Index: i})
			if t != n.term {
				n.emit(Observation{Kind: ObsCommitOldTerm, Index: i})
			}
			n.apply()
			return
		}
	}
}

func (n *Node) apply() {
	limit := min(n.commit, n.last())
	for n.applied < limit {
		n.applied++
		e := n.log[n.applied]
		n.emit(Observation{Kind: ObsApply, Index: e.Index, Term: e.Term, A: e.Cmd.Hash()})
		res, dup := n.store.Apply(e.Cmd, !n.cfg.Bugs.DoubleApplyRetry)
		if dup {
			n.emit(Observation{Kind: ObsDedupHit, Index: e.Index})
		}
		if e.Cmd.Op != kv.OpNoop && n.role == Leader {
			n.reply(Reply{ClientID: e.Cmd.ClientID, Seq: e.Cmd.Seq, Status: ReplyOK, Result: res,
				Index: e.Index, Term: e.Term}, true)
		}
	}
}

func (n *Node) onClient(cmd kv.Command) {
	if n.role != Leader {
		n.emit(Observation{Kind: ObsNotLeader})
		n.reply(Reply{ClientID: cmd.ClientID, Seq: cmd.Seq, Status: ReplyNotLeader, Leader: n.leader}, false)
		return
	}
	if cmd.Op == kv.OpGet && n.cfg.Bugs.StaleLeaderRead {
		n.emit(Observation{Kind: ObsLocalRead, A: uint64(cmd.Key)})
		n.reply(Reply{ClientID: cmd.ClientID, Seq: cmd.Seq, Status: ReplyOK,
			Result: kv.Result{OK: true, Value: n.store.Get(cmd.Key)}}, false)
		return
	}
	n.appendLocal(cmd)
	n.updateSelfMatch()
	n.maybeCommit()
	n.broadcast(false)
}

func (n *Node) onPersistDone(token uint64) {
	if token <= n.done || token > n.token {
		return
	}
	n.done = token
	k := 0
	for _, mk := range n.marks {
		if mk.token <= token {
			n.durable = mk.last
			continue
		}
		n.marks[k] = mk
		k++
	}
	n.marks = n.marks[:k]
	j := 0
	for _, h := range n.hold {
		if h.token <= token {
			n.effects = append(n.effects, h.eff)
			continue
		}
		n.hold[j] = h
		j++
	}
	n.hold = n.hold[:j]
	n.maybeCommit()
}

// flush emits this Step's persistence batch and releases or holds its gated
// outputs.
func (n *Node) flush() {
	if n.hard {
		n.batch.HardState = true
		n.batch.Term = n.term
		n.batch.Vote = n.vote
		n.hard = false
	}
	if !n.batch.Empty() {
		n.token++
		n.effects = append(n.effects, Effect{Kind: EffPersist, Batch: n.batch, Token: n.token})
		n.marks = append(n.marks, mark{token: n.token, last: n.last()})
		n.batch = PersistBatch{}
	}
	if len(n.out) == 0 {
		return
	}
	if n.done == n.token {
		n.effects = append(n.effects, n.out...)
		return
	}
	for _, e := range n.out {
		n.hold = append(n.hold, held{token: n.token, eff: e})
	}
	n.emit(Observation{Kind: ObsGated, A: uint64(len(n.out))})
}
