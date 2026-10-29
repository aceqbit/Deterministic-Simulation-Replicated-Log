package check

// OracleMax is the largest history the brute-force oracle accepts.
const OracleMax = 8

// Oracle decides linearizability of at most OracleMax ops by trying every
// order consistent with real time (a precedes b iff a.Return < b.Call) on a
// per-key register. It exists to cross-check Linearizable. ok is false when
// the history is too large.
func Oracle(ops []Op) (linearizable, ok bool) {
	if len(ops) > OracleMax {
		return false, false
	}
	var used [OracleMax]bool
	state := map[uint32]int64{}
	var try func(placed int) bool
	try = func(placed int) bool {
		if placed == len(ops) {
			return true
		}
		for i := range ops {
			if used[i] || !minimal(ops, used[:len(ops)], i) {
				continue
			}
			o := &ops[i]
			prev := state[o.Key]
			good, next := stepRegister(prev, o)
			if !good {
				continue
			}
			used[i] = true
			state[o.Key] = next
			if try(placed + 1) {
				return true
			}
			state[o.Key] = prev
			used[i] = false
		}
		return false
	}
	return try(0), true
}

// minimal reports whether op i can be placed next: no unplaced op finished
// before it was called.
func minimal(ops []Op, used []bool, i int) bool {
	for j := range ops {
		if j != i && !used[j] && ops[j].Return < ops[i].Call {
			return false
		}
	}
	return true
}
