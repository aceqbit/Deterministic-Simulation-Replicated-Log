package sim

import (
	"testing"

	"github.com/aceqbit/seedlog/core"
)

// TestBugRegression runs the seed that first exposed each injected bug in the
// pilot and expects the recorded failure signature (detector/invariant).
func TestBugRegression(t *testing.T) {
	tests := []struct {
		bug  int
		seed uint64
		sig  string
	}{
		{1, 351, "election_safety/two_leaders_same_term"},
		{2, 1, "leader_completeness/leader_missing_committed_entry"},
		{3, 1, "state_machine_safety/old_term_entry_committed_by_count"},
		{4, 1, "state_machine_safety/committed_entry_truncated"},
		{5, 1, "election_safety/two_leaders_same_term"},
		{6, 1, "commit_bound/commit_past_last_index"},
		{7, 1, "linearizability/history_not_linearizable"},
		{8, 1744, "leader_completeness/leader_missing_committed_entry"},
		{9, 1, "linearizability/history_not_linearizable"},
		{10, 2402, "wal_recovery/checksum_failing_record_accepted"},
	}
	for _, tt := range tests {
		cfg := Reference()
		cfg.Bugs, _ = core.BugByNumber(tt.bug)
		r := run(tt.seed, cfg)
		if r.Violation == nil {
			t.Errorf("bug %d seed %d: no violation", tt.bug, tt.seed)
			continue
		}
		if got := r.Violation.Signature(); got != tt.sig {
			t.Errorf("bug %d seed %d: signature %s, want %s", tt.bug, tt.seed, got, tt.sig)
		}
	}
}

// TestCorrectBuildQuiet runs a spread of seeds with every bug off.
func TestCorrectBuildQuiet(t *testing.T) {
	cfg := Reference()
	cfg.Duration, cfg.Quiet = 30*sec, 8*sec
	for seed := uint64(1); seed <= 40; seed++ {
		if r := run(seed, cfg); r.Violation != nil {
			t.Fatalf("seed %d: %v", seed, r.Violation)
		}
	}
}
