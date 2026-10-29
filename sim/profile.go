package sim

import "github.com/aceqbit/seedlog/core"

const (
	ms  = int64(1_000_000)
	sec = 1000 * ms
)

// Config describes one simulated run. Durations are nanoseconds.
type Config struct {
	Nodes          int
	Duration       int64 // simulated length of a run
	Quiet          int64 // faults stop this long before the end
	Clients        int
	ThinkMax       int64 // client think time is uniform in [0, ThinkMax)
	Keys           uint32
	Mix            [4]uint64 // put, get, cas, incr weights
	Heartbeat      int64
	MaxBatch       int // entries per AppendEntries; small batches make catch-up span several acks
	ElectionBase   int64
	ElectionJitter int64
	ClientTimeout  int64
	LivenessBudget int64
	LinTimeoutMS   int
	MaxEvents      uint64 // budget: a run past this many events stops as Budget
	Bugs           core.Bugs

	// FixedFaults, when set, replaces the per-seed fault selection with
	// FaultMask and the network parameters in Net.
	FixedFaults bool
	FaultMask   uint32
	// Quietly disables every fault; used by the election-only checks.
	NoFaults bool

	RecordDraws bool
	CheckLin    bool
	// StopOnViolation ends the run at the first online violation.
	StopOnViolation bool
}

// Reference returns the reference profile: 5 nodes, 60 simulated seconds,
// 100 client operations per simulated second (40% put, 40% get, 10% CAS,
// 10% increment over 16 keys), 50 ms heartbeat, 150 to 300 ms election
// timeout, per-seed fault selection.
func Reference() Config {
	return Config{
		Nodes:           5,
		Duration:        60 * sec,
		Quiet:           10 * sec,
		Clients:         8,
		ThinkMax:        140 * ms,
		Keys:            16,
		Mix:             [4]uint64{40, 40, 10, 10},
		Heartbeat:       50 * ms,
		MaxBatch:        4,
		ElectionBase:    150 * ms,
		ElectionJitter:  150 * ms,
		ClientTimeout:   400 * ms,
		LivenessBudget:  5 * sec,
		LinTimeoutMS:    2000,
		MaxEvents:       4_000_000,
		CheckLin:        true,
		StopOnViolation: true,
	}
}
