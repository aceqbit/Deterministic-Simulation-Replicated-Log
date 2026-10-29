package check

import (
	"fmt"
	"math"

	"github.com/aceqbit/seedlog/kv"
)

// Detector names. The set is closed: it is a metric attribute.
const (
	DetElectionSafety     = "election_safety"
	DetLeaderAppendOnly   = "leader_append_only"
	DetLogMatching        = "log_matching"
	DetLeaderCompleteness = "leader_completeness"
	DetStateMachineSafety = "state_machine_safety"
	DetCommitBound        = "commit_bound"
	DetLinearizability    = "linearizability"
	DetLiveness           = "liveness"
	DetAckLedger          = "ack_ledger"
	DetWALRecovery        = "wal_recovery"
	DetPanic              = "panic"
)

// Detectors lists every detector in a fixed order.
var Detectors = []string{
	DetElectionSafety, DetLeaderAppendOnly, DetLogMatching, DetLeaderCompleteness,
	DetStateMachineSafety, DetCommitBound, DetLinearizability, DetLiveness,
	DetAckLedger, DetWALRecovery, DetPanic,
}

// DetectorIndex returns the position of d in Detectors, or -1.
func DetectorIndex(d string) int {
	for i, x := range Detectors {
		if x == d {
			return i
		}
	}
	return -1
}

// Violation is a checker verdict. (Detector, Invariant) is the failure
// signature that shrinking and replay must reproduce.
type Violation struct {
	Detector  string
	Invariant string
	Detail    string
	Time      int64 // host time of detection, ns
	Node      int
}

// Signature returns "detector/invariant".
func (v *Violation) Signature() string { return v.Detector + "/" + v.Invariant }

func (v *Violation) Error() string {
	return fmt.Sprintf("%s: %s (node %d, t=%dns)", v.Signature(), v.Detail, v.Node, v.Time)
}

// Pending is the Return time of an operation that never completed.
const Pending = math.MaxInt64

// Op is one client operation in a history. A pending op has Return == Pending
// and an unknown result.
type Op struct {
	ClientID uint64
	Seq      uint64
	Kind     kv.OpKind
	Key      uint32
	Value    int64
	Expect   int64
	Call     int64
	Return   int64
	OK       bool  // result: CAS swapped
	Out      int64 // result: GET value, INCR new value
}

// IsPending reports whether the op has no response.
func (o *Op) IsPending() bool { return o.Return == Pending }

// History is an append-only record of client operations.
type History struct {
	Ops []Op
}

// Invoke records a call and returns its index.
func (h *History) Invoke(op Op) int {
	op.Return = Pending
	h.Ops = append(h.Ops, op)
	return len(h.Ops) - 1
}

// Complete records the response of op i.
func (h *History) Complete(i int, at int64, res kv.Result) {
	h.Ops[i].Return = at
	h.Ops[i].OK = res.OK
	h.Ops[i].Out = res.Value
}

// Reset empties the history, keeping its storage.
func (h *History) Reset() { h.Ops = h.Ops[:0] }

// Completed returns the number of completed operations.
func (h *History) Completed() int {
	n := 0
	for i := range h.Ops {
		if !h.Ops[i].IsPending() {
			n++
		}
	}
	return n
}
