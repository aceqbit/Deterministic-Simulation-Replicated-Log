package telemetry

import (
	"context"
	"errors"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Config names the process in exported telemetry.
type Config struct {
	ServiceName string
	InstanceID  string
}

// ExportInterval is how often metrics are pushed, so "no leader for 5 s" is observable.
const ExportInterval = 5 * time.Second

// Endpoint is the environment variable that enables export.
const Endpoint = "OTEL_EXPORTER_OTLP_ENDPOINT"

// Enabled reports whether an OTLP endpoint is configured.
func Enabled() bool { return os.Getenv(Endpoint) != "" }

var (
	seedBuckets    = []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5, 10, 30, 60}
	latencyBuckets = []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1}
	eventBuckets   = []float64{1e3, 1e4, 1e5, 1e6, 1e7}
)

// Setup installs the SDK and returns a shutdown function. It is a no-op when
// OTEL_EXPORTER_OTLP_ENDPOINT is unset.
func Setup(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	if !Enabled() {
		return func(context.Context) error { return nil }, nil
	}
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		semconv.ServiceName(cfg.ServiceName), semconv.ServiceInstanceID(cfg.InstanceID)))
	if err != nil {
		return nil, err
	}
	mexp, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return nil, err
	}
	texp, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}
	hist := func(name string, b []float64) sdkmetric.View {
		return sdkmetric.NewView(sdkmetric.Instrument{Name: name},
			sdkmetric.Stream{Aggregation: sdkmetric.AggregationExplicitBucketHistogram{Boundaries: b}})
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(mexp, sdkmetric.WithInterval(ExportInterval))),
		sdkmetric.WithView(
			hist("seedlog.sim.seed.duration", seedBuckets),
			hist("seedlog.sim.seed.events", eventBuckets),
			hist("seedlog.wal.fsync.duration", latencyBuckets),
			hist("seedlog.kv.request.duration", latencyBuckets),
		),
	)
	tp := sdktrace.NewTracerProvider(sdktrace.WithResource(res), sdktrace.WithBatcher(texp))
	otel.SetMeterProvider(mp)
	otel.SetTracerProvider(tp)
	Explorer = NewExplorer(mp.Meter("seedlog"))
	return func(ctx context.Context) error {
		return errors.Join(mp.Shutdown(ctx), tp.Shutdown(ctx))
	}, nil
}

// Outcomes of a seed, the closed set of the outcome attribute.
const (
	OutcomePass      = "pass"
	OutcomeViolation = "violation"
	OutcomeLiveness  = "liveness"
	OutcomePanic     = "panic"
	OutcomeBudget    = "budget"
)

// SeedStats is the plain-counter summary of one seed that a worker flushes.
type SeedStats struct {
	Outcome   string
	Events    uint64
	VirtualS  float64
	BusyS     float64
	Faults    map[string]uint64 // keyed by fault kind
	Coverage  map[string]uint64 // keyed by coverage point
	Detector  string            // set on violation
	DurationS float64
}

// ExplorerMetrics are the simulator instruments of the Explorer metrics table.
type ExplorerMetrics struct {
	seeds        metric.Int64Counter
	events       metric.Int64Counter
	virtual      metric.Float64Counter
	busy         metric.Float64Counter
	workers      metric.Int64Gauge
	seedDuration metric.Float64Histogram
	seedEvents   metric.Int64Histogram
	faults       metric.Int64Counter
	coverage     metric.Int64Counter
	violations   metric.Int64Counter
}

// Explorer is the process-wide set of instruments; no-ops until Setup runs.
var Explorer = NewExplorer(otel.Meter("seedlog"))

// NewExplorer creates the instruments on m.
func NewExplorer(m metric.Meter) *ExplorerMetrics {
	e := &ExplorerMetrics{}
	e.seeds, _ = m.Int64Counter("seedlog.sim.seeds", metric.WithDescription("seeds run, by outcome"))
	e.events, _ = m.Int64Counter("seedlog.sim.events", metric.WithDescription("simulator events"))
	e.virtual, _ = m.Float64Counter("seedlog.sim.virtual_time", metric.WithUnit("s"))
	e.busy, _ = m.Float64Counter("seedlog.sim.worker.busy", metric.WithUnit("s"))
	e.workers, _ = m.Int64Gauge("seedlog.sim.workers")
	e.seedDuration, _ = m.Float64Histogram("seedlog.sim.seed.duration", metric.WithUnit("s"))
	e.seedEvents, _ = m.Int64Histogram("seedlog.sim.seed.events")
	e.faults, _ = m.Int64Counter("seedlog.sim.faults")
	e.coverage, _ = m.Int64Counter("seedlog.sim.coverage.hits")
	e.violations, _ = m.Int64Counter("seedlog.sim.violations")
	return e
}

// SetWorkers records the number of parallel workers.
func (e *ExplorerMetrics) SetWorkers(n int) { e.workers.Record(context.Background(), int64(n)) }

// Record adds the deltas of one seed: about twenty instrument calls, none on
// the simulator hot path.
func (e *ExplorerMetrics) Record(s *SeedStats) {
	ctx := context.Background()
	e.seeds.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", s.Outcome)))
	e.events.Add(ctx, int64(s.Events))
	e.virtual.Add(ctx, s.VirtualS)
	e.busy.Add(ctx, s.BusyS)
	e.seedDuration.Record(ctx, s.DurationS)
	e.seedEvents.Record(ctx, int64(s.Events))
	for k, n := range s.Faults {
		if n > 0 {
			e.faults.Add(ctx, int64(n), metric.WithAttributes(attribute.String("kind", k)))
		}
	}
	for k, n := range s.Coverage {
		if n > 0 {
			e.coverage.Add(ctx, int64(n), metric.WithAttributes(attribute.String("point", k)))
		}
	}
	if s.Detector != "" {
		e.violations.Add(ctx, 1, metric.WithAttributes(attribute.String("detector", s.Detector)))
	}
}
