package shrink

import (
	"encoding/json"
	"os"
)

// Outcome is what one re-run of a candidate sequence reports.
type Outcome struct {
	Signature string   // "detector/invariant", empty when the run passed
	Events    uint64   // simulator events processed
	Effective []uint64 // draws actually consumed, after clamping
}

// RunFunc re-runs the simulation on a candidate choice sequence.
type RunFunc func(seq []uint64) Outcome

// Stats describes a shrink.
type Stats struct {
	Attempts     int
	Kept         int
	EventsBefore uint64
	EventsAfter  uint64
	DrawsBefore  int
	DrawsAfter   int
}

// Shrink minimizes seq, which must fail with signature sig under run. It
// stops after maxAttempts re-runs. A candidate is kept only if the same
// signature fires; the kept sequence is the draws the run actually consumed.
func Shrink(seq []uint64, sig string, run RunFunc, maxAttempts int) ([]uint64, Stats) {
	st := Stats{DrawsBefore: len(seq)}
	cur := append([]uint64(nil), seq...)
	base := run(cur)
	st.EventsBefore = base.Events
	if base.Signature != sig {
		st.EventsAfter, st.DrawsAfter = base.Events, len(cur)
		return cur, st
	}
	curEvents := base.Events
	cur = append(cur[:0], base.Effective...)

	try := func(cand []uint64) bool {
		if st.Attempts >= maxAttempts {
			return false
		}
		st.Attempts++
		o := run(cand)
		if o.Signature != sig || o.Events > curEvents {
			return false // a candidate must keep the signature and not grow the run
		}
		st.Kept++
		cur = append(cur[:0], o.Effective...)
		curEvents = o.Events
		return true
	}

	for size := len(cur) / 2; size >= 1 && st.Attempts < maxAttempts; size /= 2 {
		// Delete blocks.
		for i := 0; i+size <= len(cur) && st.Attempts < maxAttempts; {
			cand := make([]uint64, 0, len(cur)-size)
			cand = append(cand, cur[:i]...)
			cand = append(cand, cur[i+size:]...)
			if !try(cand) {
				i += size
			}
		}
		// Zero blocks.
		for i := 0; i+size <= len(cur) && st.Attempts < maxAttempts; i += size {
			if allZero(cur[i : i+size]) {
				continue
			}
			cand := append([]uint64(nil), cur...)
			clear(cand[i : i+size])
			try(cand)
		}
	}
	// Lower values.
	for i := 0; i < len(cur) && st.Attempts < maxAttempts; i++ {
		for cur[i] > 0 && st.Attempts < maxAttempts {
			cand := append([]uint64(nil), cur...)
			cand[i] /= 2
			if !try(cand) {
				break
			}
		}
	}
	st.EventsAfter, st.DrawsAfter = curEvents, len(cur)
	return cur, st
}

func allZero(s []uint64) bool {
	for _, v := range s {
		if v != 0 {
			return false
		}
	}
	return true
}

// Schedule is a minimized failing run on disk.
type Schedule struct {
	Bug       int      `json:"bug"`
	Signature string   `json:"signature"`
	Seed      uint64   `json:"seed"`
	Choices   []uint64 `json:"choices"`
}

// Save writes s to path as JSON.
func (s *Schedule) Save(path string) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// Load reads a schedule from path.
func Load(path string) (*Schedule, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Schedule
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}
