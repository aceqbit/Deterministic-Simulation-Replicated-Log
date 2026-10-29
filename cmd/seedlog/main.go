// Command seedlog runs the SeedLog simulator, explorer, replayer and shrinker.
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aceqbit/seedlog/check"
	"github.com/aceqbit/seedlog/core"
	"github.com/aceqbit/seedlog/sim"
	"github.com/aceqbit/seedlog/telemetry"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "sim":
		err = cmdSim(os.Args[2:])
	case "replay":
		err = cmdReplay(os.Args[2:])
	case "explore":
		err = cmdExplore(os.Args[2:])
	case "shrink":
		err = cmdShrink(os.Args[2:])
	case "node":
		err = cmdNode(os.Args[2:])
	case "pilot":
		err = cmdPilot(os.Args[2:])
	case "study":
		err = cmdStudy(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "seedlog:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: seedlog sim|replay|explore|shrink|study [flags]")
	os.Exit(2)
}

// config builds the run configuration from the common flags.
func config(bug int, durationS int) (sim.Config, error) {
	cfg := sim.Reference()
	b, err := core.BugByNumber(bug)
	if err != nil {
		return cfg, err
	}
	cfg.Bugs = b
	if durationS > 0 {
		cfg.Duration = int64(durationS) * 1_000_000_000
		if cfg.Quiet > cfg.Duration/3 {
			cfg.Quiet = cfg.Duration / 3
		}
	}
	return cfg, nil
}

func runSeed(cfg sim.Config, seed uint64) sim.Result {
	return sim.New(cfg, sim.NewChoiceSource(seed, cfg.RecordDraws)).Run()
}

func describe(r sim.Result) string {
	if r.Violation != nil {
		return fmt.Sprintf("violation %s: %s", r.Violation.Signature(), r.Violation.Detail)
	}
	return "ok"
}

func cmdSim(args []string) error {
	fs := flag.NewFlagSet("sim", flag.ExitOnError)
	seed := fs.Uint64("seed", 1, "seed")
	seeds := fs.Int("seeds", 0, "run seeds 1..N and print hashes")
	hashOnly := fs.Bool("hash-only", false, "print only seed and hash, one per line")
	bug := fs.Int("bug", 0, "enable bug N (1..10)")
	dur := fs.Int("duration", 0, "simulated seconds per seed")
	noFaults := fs.Bool("nofaults", false, "disable every fault")
	_ = fs.Parse(args)
	cfg, err := config(*bug, *dur)
	if err != nil {
		return err
	}
	cfg.NoFaults = *noFaults
	if *seeds > 0 {
		for sd := uint64(1); sd <= uint64(*seeds); sd++ {
			r := runSeed(cfg, sd)
			if *hashOnly {
				fmt.Printf("%d %016x\n", sd, r.Hash)
			} else {
				fmt.Printf("seed=%d leader=%d term=%d hash=%016x %s\n", sd, r.Leader, r.Term, r.Hash, describe(r))
			}
		}
		return nil
	}
	r := runSeed(cfg, *seed)
	fmt.Printf("seed=%d leader=%d term=%d hash=%016x events=%d ops=%d/%d %s\n",
		*seed, r.Leader, r.Term, r.Hash, r.Events, r.Completed, r.Ops, describe(r))
	if r.Violation != nil {
		fmt.Printf("reproduce: seedlog replay --seed %d%s\n", *seed, bugFlag(*bug))
		os.Exit(1)
	}
	return nil
}

func bugFlag(b int) string {
	if b == 0 {
		return ""
	}
	return fmt.Sprintf(" --bug %d", b)
}

func cmdReplay(args []string) error {
	fs := flag.NewFlagSet("replay", flag.ExitOnError)
	seed := fs.Uint64("seed", 1, "seed")
	bug := fs.Int("bug", 0, "enable bug N")
	dur := fs.Int("duration", 0, "simulated seconds per seed")
	schedule := fs.String("schedule", "", "replay a minimized schedule file")
	otel := fs.Bool("otel", false, "export a virtual-time trace over OTLP")
	_ = fs.Parse(args)
	if *schedule != "" {
		return replaySchedule(*schedule)
	}
	cfg, err := config(*bug, *dur)
	if err != nil {
		return err
	}
	a := runSeed(cfg, *seed)
	b := runSeed(cfg, *seed)
	fmt.Printf("seed=%d hash=%016x %s\n", *seed, a.Hash, describe(a))
	if a.Hash != b.Hash || a.Events != b.Events {
		return fmt.Errorf("replay diverged: %016x (%d events) vs %016x (%d events)", a.Hash, a.Events, b.Hash, b.Events)
	}
	fmt.Printf("replay verified: identical hash over %d events\n", a.Events)
	if *otel {
		return replayOtel(*seed, *bug, *dur)
	}
	return nil
}

func cmdExplore(args []string) error {
	fs := flag.NewFlagSet("explore", flag.ExitOnError)
	seeds := fs.Int("seeds", 1000, "number of seeds")
	start := fs.Uint64("start", 1, "first seed")
	bug := fs.Int("bug", 0, "enable bug N (stops at first failure)")
	workers := fs.Int("workers", runtime.NumCPU(), "parallel workers")
	dur := fs.Int("duration", 0, "simulated seconds per seed")
	noFaults := fs.Bool("nofaults", false, "disable every fault")
	otel := fs.Bool("otel", false, "export metrics over OTLP")
	_ = fs.Parse(args)
	cfg, err := config(*bug, *dur)
	if err != nil {
		return err
	}
	cfg.NoFaults = *noFaults
	stopOtel := setupOtel(*otel)
	defer stopOtel()
	var (
		next      atomic.Uint64
		stop      atomic.Bool
		mu        sync.Mutex
		firstSeed = ^uint64(0)
		first     sim.Result
		faults    [sim.NumFaults]uint64
		cov       [sim.NumCov]uint64
		pass      int
		viol      int
		unknown   int
		events    uint64
		virtual   int64
	)
	telemetry.Explorer.SetWorkers(max(*workers, 1))
	begin := time.Now()
	var wg sync.WaitGroup
	for w := 0; w < max(*workers, 1); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				i := next.Add(1) - 1
				if i >= uint64(*seeds) {
					return
				}
				seed := *start + i
				t0 := time.Now()
				r := runSeed(cfg, seed)
				recordOtel(&r, time.Since(t0))
				mu.Lock()
				events += r.Events
				virtual += r.VirtualNS
				for k := range faults {
					faults[k] += r.Faults[k]
				}
				for k := range cov {
					cov[k] += r.Cov[k]
				}
				switch {
				case r.Violation != nil:
					viol++
					if seed < firstSeed {
						firstSeed, first = seed, r
					}
					if *bug > 0 {
						stop.Store(true)
					}
				case r.Budget || (r.LinChecked && r.Lin == check.LinUnknown):
					unknown++
				default:
					pass++
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	wall := time.Since(begin).Seconds()
	hit := 0
	for _, c := range cov {
		if c > 0 {
			hit++
		}
	}
	fmt.Printf("seeds=%d pass=%d violations=%d unknown/budget=%d wall=%.1fs events/s=%.0f sim-s/wall-s=%.0f\n",
		pass+viol+unknown, pass, viol, unknown, wall, float64(events)/wall, float64(virtual)/1e9/wall)
	fmt.Printf("coverage: %d/%d points hit (%.1f%%)\n", hit, sim.NumCov, 100*float64(hit)/float64(sim.NumCov))
	fmt.Print("faults:")
	for k, n := range faults {
		fmt.Printf(" %s=%d", sim.FaultNames[k], n)
	}
	fmt.Println()
	if viol > 0 {
		v := first.Violation
		fmt.Printf("FIRST FAILURE detector=%s invariant=%s seed=%d events=%d\n  %s\n", v.Detector, v.Invariant, firstSeed, first.Events, v.Detail)
		fmt.Printf("reproduce: seedlog replay --seed %d%s\n", firstSeed, bugFlag(*bug))
		if *bug == 0 {
			os.Exit(1)
		}
	}
	return nil
}
