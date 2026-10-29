package check

// Liveness verifies bounded liveness: after all faults heal at healAt, a
// leader exists at the end of the run and some client operation completes
// within budget of healAt. Times are host nanoseconds.
func Liveness(h *History, healAt, budget, end int64, leaderAtEnd bool) *Violation {
	if end < healAt+budget {
		return nil
	}
	if !leaderAtEnd {
		return &Violation{Detector: DetLiveness, Invariant: "no_leader_after_heal", Time: end, Node: -1,
			Detail: "no live leader at the end of the run"}
	}
	for i := range h.Ops {
		o := &h.Ops[i]
		if !o.IsPending() && o.Return > healAt && o.Return <= healAt+budget {
			return nil
		}
	}
	return &Violation{Detector: DetLiveness, Invariant: "no_progress_after_heal", Time: end, Node: -1,
		Detail: "no client operation completed within the budget after faults healed"}
}
