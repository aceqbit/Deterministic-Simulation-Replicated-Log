package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/aceqbit/seedlog/core"
	"github.com/aceqbit/seedlog/sim"
	"github.com/aceqbit/seedlog/telemetry"
)

var otelOn bool

// setupOtel installs the SDK when --otel is set and an endpoint is configured.
func setupOtel(enable bool) func() {
	if !enable {
		return func() {}
	}
	if !telemetry.Enabled() {
		fmt.Fprintln(os.Stderr, "seedlog: --otel set but OTEL_EXPORTER_OTLP_ENDPOINT is empty; telemetry stays off")
		return func() {}
	}
	shutdown, err := telemetry.Setup(context.Background(), telemetry.Config{ServiceName: "seedlog", InstanceID: strconv.Itoa(os.Getpid())})
	if err != nil {
		fmt.Fprintln(os.Stderr, "seedlog: telemetry:", err)
		return func() {}
	}
	otelOn = true
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = shutdown(ctx)
	}
}

// recordOtel flushes one seed's counters; it runs after the seed, never inside it.
func recordOtel(r *sim.Result, busy time.Duration) {
	if !otelOn {
		return
	}
	st := telemetry.SeedStats{
		Outcome: telemetry.OutcomePass, Events: r.Events, VirtualS: float64(r.VirtualNS) / 1e9,
		BusyS: busy.Seconds(), DurationS: busy.Seconds(),
		Faults: map[string]uint64{}, Coverage: map[string]uint64{},
	}
	for k, n := range r.Faults {
		st.Faults[sim.FaultNames[k]] = n
	}
	for k, n := range r.Cov {
		st.Coverage[sim.CovNames[k]] = n
	}
	switch {
	case r.Budget:
		st.Outcome = telemetry.OutcomeBudget
	case r.Violation != nil:
		st.Outcome, st.Detector = telemetry.OutcomeViolation, r.Violation.Detector
		switch r.Violation.Detector {
		case "liveness":
			st.Outcome = telemetry.OutcomeLiveness
		case "panic":
			st.Outcome = telemetry.OutcomePanic
		}
	}
	telemetry.Explorer.Record(&st)
}

// replayOtel re-runs the seed with tracing hooks and exports one trace.
func replayOtel(seed uint64, bug int, dur int) error {
	stop := setupOtel(true)
	defer stop()
	cfg, err := config(bug, dur)
	if err != nil {
		return err
	}
	var spans []telemetry.SpanRec
	var events []telemetry.EventRec
	electing := map[int]int64{}
	s := sim.New(cfg, sim.NewChoiceSource(seed, false))
	s.OnObserve = func(node int, at int64, o core.Observation) {
		switch o.Kind {
		case core.ObsElectionStart:
			if _, ok := electing[node]; !ok {
				electing[node] = at
			}
		case core.ObsState:
			if st, ok := electing[node]; ok && core.Role(o.A) != core.Candidate {
				spans = append(spans, telemetry.SpanRec{Name: "election", Node: node, Start: st, End: at,
					Attrs: []attribute.KeyValue{attribute.Int64("term", int64(o.Term)), attribute.String("role", core.Role(o.A).String())}})
				delete(electing, node)
			}
		}
	}
	s.Hooks = sim.Hooks{
		Message: func(from, to int, typ core.MsgType, start, end int64) {
			spans = append(spans, telemetry.SpanRec{Name: "message", Node: from, Start: start, End: end,
				Attrs: []attribute.KeyValue{attribute.Int("to", to), attribute.Int("type", int(typ))}})
		},
		Fsync: func(node int, start, end int64) {
			spans = append(spans, telemetry.SpanRec{Name: "fsync", Node: node, Start: start, End: end})
		},
		Fault: func(kind string, node int, at int64) {
			events = append(events, telemetry.EventRec{Name: kind, Node: node, At: at})
		},
	}
	epoch := time.Now()
	r := s.Run()
	id := telemetry.EmitReplay(context.Background(), seed, epoch, r.VirtualNS, spans, events)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	logger.Info("replay", "seed", seed, "node", -1, "trace_id", id, "hash", fmt.Sprintf("%016x", r.Hash), "result", describe(r))
	return nil
}
