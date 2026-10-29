package sim

import (
	"testing"

	"github.com/aceqbit/seedlog/core"
)

// TestNoMessageStorm guards against duplicated acks multiplying send
// pipelines: seed 23 with bug 8 once queued over a million events.
func TestNoMessageStorm(t *testing.T) {
	cfg := Reference()
	cfg.Bugs, _ = core.BugByNumber(8)
	s := New(cfg, NewChoiceSource(23, false))
	s.Run()
	if s.hp.len() > 100000 {
		t.Fatalf("event heap holds %d entries at the end of the run", s.hp.len())
	}
}
