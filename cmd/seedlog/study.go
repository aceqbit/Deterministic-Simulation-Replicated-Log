package main

import (
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aceqbit/seedlog/core"
	"github.com/aceqbit/seedlog/sim"
)

// found is the first failure of a bug search.
type found struct {
	seed   uint64
	res    sim.Result
	tried  uint64 // seeds run up to and including the failing one
	wall   time.Duration
	cpu    time.Duration // summed busy time of the workers
	failed bool
}

// findFailure runs seeds start, start+1, ... until one fails or n seeds ran.
// The failure reported is the one with the lowest seed among those found.
func findFailure(cfg sim.Config, start uint64, n int, workers int) found {
	var (
		next    atomic.Uint64
		stop    atomic.Bool
		mu      sync.Mutex
		best    = found{seed: ^uint64(0)}
		busy    atomic.Int64
		wg      sync.WaitGroup
		begin   = time.Now()
		limit   = uint64(n)
		current = make([]uint64, max(workers, 1))
	)
	for w := 0; w < max(workers, 1); w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for !stop.Load() {
				i := next.Add(1) - 1
				if i >= limit {
					return
				}
				t0 := time.Now()
				r := runSeed(cfg, start+i)
				busy.Add(int64(time.Since(t0)))
				if r.Violation != nil {
					mu.Lock()
					if start+i < best.seed {
						best = found{seed: start + i, res: r, failed: true}
					}
					mu.Unlock()
					stop.Store(true)
				}
				current[w] = i
			}
		}(w)
	}
	wg.Wait()
	best.wall = time.Since(begin)
	best.cpu = time.Duration(busy.Load())
	if best.failed {
		best.tried = best.seed - start + 1
	} else {
		best.tried = uint64(n)
	}
	return best
}

var expectedDetector = [10]string{
	"election_safety", "leader_completeness", "state_machine_safety", "log_matching",
	"liveness", "commit_bound", "linearizability", "ack_ledger", "linearizability", "wal_recovery",
}

func cmdPilot(args []string) error {
	fs := flag.NewFlagSet("pilot", flag.ExitOnError)
	seeds := fs.Int("seeds", 10000, "seeds per bug")
	workers := fs.Int("workers", runtime.NumCPU(), "workers")
	out := fs.String("out", "study/pilot.csv", "output CSV")
	_ = fs.Parse(args)
	f, err := os.Create(out2(*out))
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	w := csv.NewWriter(f)
	_ = w.Write([]string{"bug", "flag", "seeds_to_detect", "first_detector", "first_invariant", "expected_detector"})
	for bug := 1; bug <= 10; bug++ {
		cfg, _ := config(bug, 0)
		r := findFailure(cfg, 1, *seeds, *workers)
		det, inv, n := "none", "none", "none"
		if r.failed {
			det, inv, n = r.res.Violation.Detector, r.res.Violation.Invariant, strconv.FormatUint(r.tried, 10)
		}
		fmt.Printf("bug %2d %-20s seeds_to_detect=%s first=%s/%s expected=%s\n", bug, core.BugNames[bug-1], n, det, inv, expectedDetector[bug-1])
		_ = w.Write([]string{strconv.Itoa(bug), core.BugNames[bug-1], n, det, inv, expectedDetector[bug-1]})
	}
	w.Flush()
	return w.Error()
}

func out2(p string) string {
	_ = os.MkdirAll("study", 0o755)
	return p
}

func cmdStudy(args []string) error {
	fs := flag.NewFlagSet("study", flag.ExitOnError)
	method := fs.String("method", "sim", "sim or stress")
	bug := fs.Int("bug", 0, "single bug, 0 for all ten")
	trials := fs.Int("trials", 30, "trials per bug")
	budget := fs.Int("budget", 120, "wall-clock budget per trial, seconds")
	cpus := fs.Int("cpus", 1, "CPU allowance per trial (worker count)")
	ledger := fs.String("ledger", "study/ledger.csv", "ledger CSV to append")
	_ = fs.Parse(args)
	if *method != "sim" {
		return errors.New("study: stress method needs the Docker cluster (runtime/, deploy/compose); not available in this build")
	}
	_ = os.MkdirAll("study", 0o755)
	_, statErr := os.Stat(*ledger)
	f, err := os.OpenFile(*ledger, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	w := csv.NewWriter(f)
	if statErr != nil {
		_ = w.Write([]string{"bug", "method", "trial", "outcome", "wall_s", "cpu_s", "seeds_or_schedules", "detector", "signature", "binary_hash", "config_hash"})
	}
	lo, hi := 1, 10
	if *bug > 0 {
		lo, hi = *bug, *bug
	}
	for b := lo; b <= hi; b++ {
		cfg, err := config(b, 0)
		if err != nil {
			return err
		}
		for t := 0; t < *trials; t++ {
			start := uint64(t)*1_000_000 + 1 // independent seed offset per trial
			r := runBudget(cfg, start, *cpus, time.Duration(*budget)*time.Second)
			outcome, det, sig := "censored", "", ""
			if r.failed {
				outcome, det, sig = "found", r.res.Violation.Detector, r.res.Violation.Signature()
			}
			_ = w.Write([]string{strconv.Itoa(b), "sim", strconv.Itoa(t), outcome,
				fmt.Sprintf("%.3f", r.wall.Seconds()), fmt.Sprintf("%.3f", r.cpu.Seconds()),
				strconv.FormatUint(r.tried, 10), det, sig, "dev", fmt.Sprintf("bug%d-%dnodes", b, cfg.Nodes)})
			w.Flush()
			fmt.Printf("bug %d trial %d %s seeds=%d wall=%.1fs\n", b, t, outcome, r.tried, r.wall.Seconds())
		}
	}
	return w.Error()
}

// runBudget searches seeds from start until a failure or the wall budget.
func runBudget(cfg sim.Config, start uint64, workers int, budget time.Duration) found {
	begin := time.Now()
	var total found
	batch := 8 * max(workers, 1)
	for time.Since(begin) < budget {
		r := findFailure(cfg, start+total.tried, batch, workers)
		total.tried += r.tried
		total.cpu += r.cpu
		if r.failed {
			total.failed, total.res, total.seed = true, r.res, r.seed
			break
		}
	}
	total.wall = time.Since(begin)
	return total
}
