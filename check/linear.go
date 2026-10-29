package check

import (
	"sort"
	"time"

	"github.com/anishathalye/porcupine"

	"github.com/aceqbit/seedlog/kv"
)

// LinResult is a linearizability verdict.
type LinResult int

// Verdicts. Unknown means the checker timed out and is never a pass.
const (
	LinOK LinResult = iota
	LinIllegal
	LinUnknown
)

// String returns the verdict name.
func (r LinResult) String() string {
	switch r {
	case LinOK:
		return "ok"
	case LinIllegal:
		return "illegal"
	}
	return "unknown"
}

// stepRegister applies op to a register holding state. It returns whether a
// completed op's recorded result is consistent and the next state.
func stepRegister(state int64, op *Op) (bool, int64) {
	known := !op.IsPending()
	switch op.Kind {
	case kv.OpPut:
		return true, op.Value
	case kv.OpGet:
		return !known || op.Out == state, state
	case kv.OpCAS:
		swap := state == op.Expect
		if known && op.OK != swap {
			return false, state
		}
		if swap {
			return true, op.Value
		}
		return true, state
	case kv.OpIncr:
		next := state + op.Value
		return !known || op.Out == next, next
	}
	return true, state
}

var registerModel = porcupine.Model{
	Partition: func(h []porcupine.Operation) [][]porcupine.Operation {
		byKey := map[uint32][]porcupine.Operation{}
		var keys []uint32
		for _, o := range h {
			k := o.Input.(*Op).Key
			if _, ok := byKey[k]; !ok {
				keys = append(keys, k)
			}
			byKey[k] = append(byKey[k], o)
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
		out := make([][]porcupine.Operation, 0, len(keys))
		for _, k := range keys {
			out = append(out, byKey[k])
		}
		return out
	},
	Init: func() interface{} { return int64(0) },
	Step: func(state, input, _ interface{}) (bool, interface{}) {
		ok, next := stepRegister(state.(int64), input.(*Op))
		return ok, next
	},
	Equal: func(a, b interface{}) bool { return a.(int64) == b.(int64) },
	Hash:  func(s interface{}) uint64 { return uint64(s.(int64)) },
}

// Linearizable checks ops against a per-key register model with Porcupine.
// Pending GETs are dropped (they cannot constrain state); other pending ops
// may take effect at any point after their call.
func Linearizable(ops []Op, timeout time.Duration) LinResult {
	hist := make([]porcupine.Operation, 0, len(ops))
	for i := range ops {
		o := &ops[i]
		if o.IsPending() && o.Kind == kv.OpGet {
			continue
		}
		hist = append(hist, porcupine.Operation{ClientId: int(o.ClientID), Input: o, Call: o.Call, Return: o.Return})
	}
	switch porcupine.CheckOperationsTimeout(registerModel, hist, timeout) {
	case porcupine.Ok:
		return LinOK
	case porcupine.Illegal:
		return LinIllegal
	}
	return LinUnknown
}

// LinearizableMS is Linearizable with a timeout in milliseconds, for callers
// that must not import time.
func LinearizableMS(ops []Op, ms int) LinResult {
	return Linearizable(ops, time.Duration(ms)*time.Millisecond)
}
