// integer clock, a PCG choice source that records every draw, network, disk
// and process fault models, a nemesis, a client workload, and protocol
// coverage points. It is single-threaded and uses no floats, goroutines or
// clock.
package sim

import (
	"fmt"

	"github.com/aceqbit/seedlog/check"
	"github.com/aceqbit/seedlog/core"
)

const maxNodes = 8

// Result is the outcome of one run.
type Result struct {
	Hash          uint64
	Events        uint64
	Draws         uint64
	Leader        int
	Term          uint64
	VirtualNS     int64
	Violation     *check.Violation
	Lin           check.LinResult
	Budget        bool // the run hit Config.MaxEvents and was cut short
	LinChecked    bool
	Ops           int
	Completed     int
	Faults        [NumFaults]uint64
	Cov           [NumCov]uint64
	Mask          uint32
	LeaderChanges uint64
	Choices       []uint64
	History       []check.Op
}

type simNode struct {
	n           *core.Node
	alive       bool
	gen         uint32
	timerGen    [core.NumTimers]uint64
	pausedUntil int64
	skew        int64 // ppm
	disk        disk
	syncDone    int64
	catchUp     bool
	recovered   uint64
}

type link struct{ base, jit int64 }

// Sim is one simulation. Create it with New and run it once with Run.
type Sim struct {
	cfg   Config
	src   *ChoiceSource
	hp    heap
	now   int64
	nodes []simNode

	msgs    []core.Message
	freeMsg []int32
	reps    []core.Reply
	freeRep []int32

	lat       [maxNodes][maxNodes]link
	blocked   [maxNodes][maxNodes]bool
	dropPm    uint64
	dupPm     uint64
	reorderPm uint64
	mask      uint32

	th      *traceHash
	on      *check.Online
	led     check.Ledger
	hist    check.History
	clients []client

	roles       []core.Role
	lastLeader  int
	afterChange bool
	sawCrash    bool
	healAt      int64
	healed      bool
	viol        *check.Violation
	scratch     []byte

	// OnObserve, when set, sees every core observation with the virtual time
	// (used by replay tracing and debugging; it must not steer the run).
	OnObserve func(node int, at int64, o core.Observation)
	// Hooks are optional replay-tracing callbacks; they must not steer the run.
	Hooks Hooks
	res   Result
}

// New returns a simulation of cfg drawing from src.
func New(cfg Config, src *ChoiceSource) *Sim {
	return &Sim{cfg: cfg, src: src, th: newTraceHash(), on: check.NewOnline(cfg.Nodes), lastLeader: core.None}
}

func (s *Sim) enabled(kind int) bool { return s.mask&(1<<kind) != 0 }

func (s *Sim) fail(det, inv string, node int, format string, args ...any) {
	if s.viol == nil {
		s.viol = &check.Violation{Detector: det, Invariant: inv, Detail: fmt.Sprintf(format, args...), Time: s.now, Node: node}
	}
}

func (s *Sim) coreConfig(i int) core.Config {
	return core.Config{ID: i, N: s.cfg.Nodes, ElectionBase: s.cfg.ElectionBase, ElectionJitter: s.cfg.ElectionJitter,
		Heartbeat: s.cfg.Heartbeat, MaxBatch: s.cfg.MaxBatch, Bugs: s.cfg.Bugs}
}

func (s *Sim) setup() {
	cfg := &s.cfg
	s.healAt = cfg.Duration - cfg.Quiet
	s.nodes = make([]simNode, cfg.Nodes)
	s.roles = make([]core.Role, cfg.Nodes)
	switch {
	case cfg.NoFaults:
		s.mask = 0
	case cfg.FixedFaults:
		s.mask = cfg.FaultMask
	default:
		for k := 0; k < NumFaults; k++ {
			if s.src.Draw(100) < 55 {
				s.mask |= 1 << k
			}
		}
	}
	s.res.Mask = s.mask
	if s.enabled(FaultDrop) {
		s.dropPm = 10 + s.src.Draw(140)
	}
	if s.enabled(FaultDup) {
		s.dupPm = 10 + s.src.Draw(90)
	}
	if s.enabled(FaultReorder) {
		s.reorderPm = 20 + s.src.Draw(180)
	}
	for a := 0; a < cfg.Nodes; a++ {
		for b := 0; b < cfg.Nodes; b++ {
			if a != b {
				s.lat[a][b] = link{base: 300_000 + int64(s.src.Draw(2*uint64(ms))), jit: 1 + int64(s.src.Draw(2*uint64(ms)))}
			}
		}
	}
	for i := range s.nodes {
		if s.enabled(FaultSkew) {
			sk := int64(s.src.Draw(100001)) - 50000
			s.nodes[i].skew = sk
			if sk != 0 {
				s.res.Faults[FaultSkew]++
				s.res.Cov[CovSkew]++
			}
		}
		s.nodes[i].alive = true
		s.nodes[i].n = core.NewNode(s.coreConfig(i), core.PersistentState{Vote: core.None})
	}
	for i := range s.nodes {
		s.step(i, core.Event{Kind: core.EvStart})
	}
	s.setupClients()
	if !cfg.NoFaults {
		s.hp.push(event{at: s.nextNemesis(), kind: evNemesis})
		s.hp.push(event{at: s.healAt, kind: evHeal})
	}
	s.hp.push(event{at: s.now + s.cfg.Duration/60, kind: evCheck})
}

func (s *Sim) nextNemesis() int64 {
	return s.now + 200*ms + int64(s.src.Draw(uint64(2*sec)))
}

// Hooks lets a replay trace see host-level activity in virtual time.
type Hooks struct {
	Message func(from, to int, typ core.MsgType, start, end int64)
	Fsync   func(node int, start, end int64)
	Fault   func(kind string, node int, at int64)
}
