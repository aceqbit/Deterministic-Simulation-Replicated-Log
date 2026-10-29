package sim

import "math/rand/v2"

// ChoiceSource is the only source of randomness in a simulation. Every draw
// is a value in [0, n) and may be recorded. A replaying source returns the
// recorded values (clamped to the draw's range) and 0 once the sequence ends.
type ChoiceSource struct {
	rng       *rand.Rand
	record    bool
	replaying bool
	replay    []uint64
	pos       int
	count     uint64
	log       []uint64
}

// NewChoiceSource returns a PCG-backed source for seed.
func NewChoiceSource(seed uint64, record bool) *ChoiceSource {
	return &ChoiceSource{rng: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)), record: record}
}

// NewReplaySource returns a source that replays seq.
func NewReplaySource(seq []uint64, record bool) *ChoiceSource {
	return &ChoiceSource{replaying: true, replay: seq, record: record}
}

// Reset re-seeds the source for a new run, keeping its log storage.
func (c *ChoiceSource) Reset(seed uint64, record bool) {
	c.rng = rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	c.record, c.replaying, c.replay, c.pos, c.count = record, false, nil, 0, 0
	c.log = c.log[:0]
}

// Draw returns a value in [0, n). A draw with n <= 1 is not a choice: it
// returns 0 and is neither counted nor recorded.
func (c *ChoiceSource) Draw(n uint64) uint64 {
	if n <= 1 {
		return 0
	}
	var v uint64
	if c.replaying {
		if c.pos < len(c.replay) {
			v = c.replay[c.pos] % n
		}
		c.pos++
	} else {
		v = c.rng.Uint64N(n)
	}
	c.count++
	if c.record {
		c.log = append(c.log, v)
	}
	return v
}

// Count returns the number of draws so far.
func (c *ChoiceSource) Count() uint64 { return c.count }

// Log returns the recorded draws.
func (c *ChoiceSource) Log() []uint64 { return c.log }
