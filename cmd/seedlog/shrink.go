package main

import (
	"flag"
	"fmt"
	"time"

	"github.com/aceqbit/seedlog/shrink"
	"github.com/aceqbit/seedlog/sim"
)

// replayChoices re-runs cfg on a recorded choice sequence.
func replayChoices(cfg sim.Config, seq []uint64) sim.Result {
	cfg.RecordDraws = true
	return sim.New(cfg, sim.NewReplaySource(seq, true)).Run()
}

func signature(r sim.Result) string {
	if r.Violation == nil {
		return ""
	}
	return r.Violation.Signature()
}

func cmdShrink(args []string) error {
	fs := flag.NewFlagSet("shrink", flag.ExitOnError)
	seed := fs.Uint64("seed", 1, "failing seed")
	bug := fs.Int("bug", 0, "bug number the seed fails with")
	out := fs.String("out", "", "schedule file (default schedule-<seed>.json)")
	attempts := fs.Int("attempts", 3000, "maximum re-runs")
	_ = fs.Parse(args)
	cfg, err := config(*bug, 0)
	if err != nil {
		return err
	}
	cfg.RecordDraws = true
	first := runSeed(cfg, *seed)
	if first.Violation == nil {
		return fmt.Errorf("seed %d does not fail (bug %d)", *seed, *bug)
	}
	sig := first.Violation.Signature()
	begin := time.Now()
	seq, st := shrink.Shrink(first.Choices, sig, func(seq []uint64) shrink.Outcome {
		r := replayChoices(cfg, seq)
		return shrink.Outcome{Signature: signature(r), Events: r.Events, Effective: r.Choices}
	}, *attempts)
	path := *out
	if path == "" {
		path = fmt.Sprintf("schedule-%d.json", *seed)
	}
	if err := (&shrink.Schedule{Bug: *bug, Signature: sig, Seed: *seed, Choices: seq}).Save(path); err != nil {
		return err
	}
	fmt.Printf("signature=%s events %d -> %d (%.1f%% removed) draws %d -> %d attempts=%d kept=%d time=%.1fs\nschedule: %s\nreproduce: seedlog replay --schedule %s\n",
		sig, st.EventsBefore, st.EventsAfter, 100*(1-float64(st.EventsAfter)/float64(max(st.EventsBefore, 1))),
		st.DrawsBefore, st.DrawsAfter, st.Attempts, st.Kept, time.Since(begin).Seconds(), path, path)
	return nil
}

func replaySchedule(path string) error {
	s, err := shrink.Load(path)
	if err != nil {
		return err
	}
	cfg, err := config(s.Bug, 0)
	if err != nil {
		return err
	}
	r := replayChoices(cfg, s.Choices)
	fmt.Printf("schedule %s: events=%d signature=%q %s\n", path, r.Events, signature(r), describe(r))
	if signature(r) != s.Signature {
		return fmt.Errorf("schedule did not reproduce %s", s.Signature)
	}
	fmt.Println("reproduced", s.Signature)
	return nil
}
