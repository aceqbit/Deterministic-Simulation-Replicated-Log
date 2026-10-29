package check

import (
	"fmt"

	"github.com/aceqbit/seedlog/core"
)

// Observer consumes core observations from a host.
type Observer interface {
	Observe(at int64, o core.Observation)
}

type rec struct {
	term, hash uint64
}

type truncation struct {
	from uint64
	term uint64 // term of the leader whose append caused the truncation
	old  []rec  // entries removed, starting at from
}

// Online checks the Raft safety invariants incrementally from the
// observations of every node: election safety, leader append-only, log
// matching, leader completeness, state-machine safety and the commit bound.
// Hosts call EndStep after delivering the observations of one Step.
type Online struct {
	logs       [][]rec
	roles      []core.Role
	terms      []uint64
	nodeCommit []uint64
	pending    []truncation
	leaderOf   map[uint64]int
	byIT       map[[2]uint64]uint64
	committed  []rec
	commitTerm []uint64 // term in which each committed index was first committed
	now        int64
	first      *Violation
}

// NewOnline returns a checker for n nodes.
func NewOnline(n int) *Online {
	o := &Online{
		logs:       make([][]rec, n),
		roles:      make([]core.Role, n),
		terms:      make([]uint64, n),
		nodeCommit: make([]uint64, n),
		pending:    make([]truncation, n),
		leaderOf:   map[uint64]int{},
		byIT:       map[[2]uint64]uint64{},
	}
	return o
}

// Reset clears all state for a new run, keeping storage.
func (o *Online) Reset() {
	for i := range o.logs {
		o.logs[i] = o.logs[i][:0]
		o.roles[i] = core.Follower
		o.terms[i] = 0
		o.nodeCommit[i] = 0
		o.pending[i] = truncation{}
	}
	clear(o.leaderOf)
	clear(o.byIT)
	o.committed = o.committed[:0]
	o.commitTerm = o.commitTerm[:0]
	o.first = nil
}

// Violation returns the first violation seen, or nil.
func (o *Online) Violation() *Violation { return o.first }

// Committed returns the number of entries known committed.
func (o *Online) Committed() int { return len(o.committed) }

// Report records an externally detected violation if none is recorded yet.
func (o *Online) Report(v *Violation) {
	if o.first == nil {
		o.first = v
	}
}

func (o *Online) fail(det, inv string, node int, format string, args ...any) {
	if o.first == nil {
		o.first = &Violation{Detector: det, Invariant: inv, Detail: fmt.Sprintf(format, args...), Time: o.now, Node: node}
	}
}

// Observe implements Observer.
func (o *Online) Observe(at int64, ob core.Observation) {
	o.now = at
	n := ob.Node
	switch ob.Kind {
	case core.ObsRestore:
		o.logs[n] = o.logs[n][:0]
		o.nodeCommit[n] = 0
		o.roles[n] = core.Follower
		o.terms[n] = ob.Term
		o.pending[n] = truncation{}
	case core.ObsState:
		o.onState(n, ob.Term, core.Role(ob.A))
	case core.ObsAppend:
		o.onAppend(n, ob.Index, rec{ob.Term, ob.A})
	case core.ObsTruncate:
		o.onTruncate(n, ob.Index, ob.Term)
	case core.ObsCommit:
		o.onCommit(n, ob.Index)
	case core.ObsCommitOldTerm:
		o.fail(DetStateMachineSafety, "old_term_entry_committed_by_count", n,
			"leader of term %d committed index %d, an older-term entry, by replica count", o.terms[n], ob.Index)
	case core.ObsApply:
		if i := ob.Index; i <= uint64(len(o.committed)) && o.committed[i-1] != (rec{ob.Term, ob.A}) {
			o.fail(DetStateMachineSafety, "applied_entry_differs", n, "index %d applied term %d, committed term %d", i, ob.Term, o.committed[i-1].term)
		}
	}
}

func (o *Online) onState(n int, term uint64, role core.Role) {
	became := role == core.Leader && (o.roles[n] != core.Leader || o.terms[n] != term)
	o.roles[n] = role
	o.terms[n] = term
	if !became {
		return
	}
	if other, ok := o.leaderOf[term]; ok && other != n {
		o.fail(DetElectionSafety, "two_leaders_same_term", n, "nodes %d and %d both lead term %d", other, n, term)
		return
	}
	o.leaderOf[term] = n
	log := o.logs[n]
	for i, c := range o.committed {
		if o.commitTerm[i] >= term {
			continue // only entries committed in earlier terms bind this leader
		}
		if i >= len(log) || log[i] != c {
			o.fail(DetLeaderCompleteness, "leader_missing_committed_entry", n, "leader of term %d lacks committed index %d", term, i+1)
			return
		}
	}
}

func (o *Online) onAppend(n int, idx uint64, r rec) {
	log := o.logs[n]
	if idx == 0 || idx > uint64(len(log))+1 {
		o.fail(DetLogMatching, "append_gap", n, "append index %d to log of %d", idx, len(log))
		return
	}
	log = append(log[:idx-1], r)
	o.logs[n] = log
	k := [2]uint64{idx, r.term}
	if h, ok := o.byIT[k]; ok {
		if h != r.hash {
			o.fail(DetLogMatching, "same_index_term_different_entry", n, "index %d term %d holds two different entries", idx, r.term)
		}
	} else {
		o.byIT[k] = r.hash
	}
}

func (o *Online) onTruncate(n int, from, term uint64) {
	if o.roles[n] == core.Leader {
		o.fail(DetLeaderAppendOnly, "leader_truncated_log", n, "leader of term %d truncated from %d", o.terms[n], from)
	}
	log := o.logs[n]
	if from == 0 || from > uint64(len(log)) {
		return
	}
	p := &o.pending[n]
	if o.nodeCommit[n] >= from {
		o.fail(DetStateMachineSafety, "committed_entry_truncated", n, "node committed through %d but truncated from %d", o.nodeCommit[n], from)
	}
	if p.old == nil || from < p.from {
		p.from = from
		p.term = term
		p.old = append([]rec(nil), log[from-1:]...)
	}
	o.logs[n] = log[:from-1]
}

func (o *Online) onCommit(n int, idx uint64) {
	log := o.logs[n]
	if idx > uint64(len(log)) {
		o.fail(DetCommitBound, "commit_past_last_index", n, "commit %d beyond last index %d", idx, len(log))
		return
	}
	for i := o.nodeCommit[n] + 1; i <= idx; i++ {
		r := log[i-1]
		switch {
		case i <= uint64(len(o.committed)):
			if o.committed[i-1] != r {
				o.fail(DetStateMachineSafety, "committed_entry_replaced", n, "index %d committed with term %d, now term %d", i, o.committed[i-1].term, r.term)
				return
			}
		default:
			o.committed = append(o.committed, r)
			o.commitTerm = append(o.commitTerm, o.terms[n])
		}
	}
	if idx > o.nodeCommit[n] {
		o.nodeCommit[n] = idx
	}
}

// EndStep finishes the checks that span one Step of node n: a truncation is
// legal only if every committed entry it removed was put back.
func (o *Online) EndStep(n int) {
	p := &o.pending[n]
	if p.old == nil {
		return
	}
	log := o.logs[n]
	for k, r := range p.old {
		i := p.from + uint64(k)
		if i > uint64(len(o.committed)) {
			break
		}
		// A delayed append from a leader of an earlier term than the commit
		// term may legally remove an entry from a node that has not learned
		// the commit; a leader at or after the commit term may not.
		if r == o.committed[i-1] && (i > uint64(len(log)) || log[i-1] != r) && p.term >= o.commitTerm[i-1] {
			o.fail(DetStateMachineSafety, "committed_entry_truncated", n, "committed index %d removed from log", i)
			break
		}
	}
	*p = truncation{}
}

// NodeCommit returns the commit index node n last reported.
func (o *Online) NodeCommit(n int) uint64 { return o.nodeCommit[n] }

// NodeEntry returns node n's mirrored entry at index i.
func (o *Online) nodeEntry(n int, i uint64) (rec, bool) {
	if i == 0 || i > uint64(len(o.logs[n])) {
		return rec{}, false
	}
	return o.logs[n][i-1], true
}
