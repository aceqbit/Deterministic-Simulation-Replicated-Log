package sim

import "testing"

func run(seed uint64, cfg Config) Result {
	return New(cfg, NewChoiceSource(seed, cfg.RecordDraws)).Run()
}

func TestRunDeterministic(t *testing.T) {
	cfg := Reference()
	cfg.Duration = 20 * sec
	cfg.Quiet = 5 * sec
	for seed := uint64(1); seed <= 20; seed++ {
		a, b := run(seed, cfg), run(seed, cfg)
		if a.Hash != b.Hash || a.Events != b.Events {
			t.Fatalf("seed %d: hash %x vs %x", seed, a.Hash, b.Hash)
		}
		if a.Violation != nil {
			t.Fatalf("seed %d: %v", seed, a.Violation)
		}
	}
}

func TestElectsLeaderNoFaults(t *testing.T) {
	cfg := Reference()
	cfg.NoFaults = true
	cfg.Duration = 5 * sec
	for seed := uint64(1); seed <= 50; seed++ {
		r := run(seed, cfg)
		if r.Violation != nil || r.Leader < 0 || r.Term == 0 {
			t.Fatalf("seed %d: leader %d term %d viol %v", seed, r.Leader, r.Term, r.Violation)
		}
	}
}

func TestReplaySourceReproduces(t *testing.T) {
	cfg := Reference()
	cfg.Duration = 10 * sec
	cfg.Quiet = 3 * sec
	cfg.RecordDraws = true
	a := run(7, cfg)
	b := New(cfg, NewReplaySource(a.Choices, true)).Run()
	if a.Hash != b.Hash {
		t.Fatalf("replayed hash %x != %x", b.Hash, a.Hash)
	}
}
