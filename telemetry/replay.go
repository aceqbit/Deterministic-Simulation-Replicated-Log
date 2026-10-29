package telemetry

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// SpanRec is one span in virtual time, nanoseconds from the start of the seed.
type SpanRec struct {
	Name       string
	Node       int
	Start, End int64
	Attrs      []attribute.KeyValue
}

// EventRec is a span event (partition, crash, torn write) in virtual time.
type EventRec struct {
	Name string
	Node int
	At   int64
}

// EmitReplay exports one trace for a replayed seed. Spans are stamped in
// virtual time with trace.WithTimestamp; the virtual epoch is epoch, the
// replay's wall-clock start, so spans sit in normal query windows. It returns
// the trace ID for log correlation.
func EmitReplay(ctx context.Context, seed uint64, epoch time.Time, endNS int64, spans []SpanRec, events []EventRec) string {
	tr := otel.Tracer("seedlog.replay")
	at := func(ns int64) time.Time { return epoch.Add(time.Duration(ns)) }
	ctx, root := tr.Start(ctx, "seed", trace.WithTimestamp(at(0)),
		trace.WithAttributes(attribute.Int64("seed", int64(seed))))
	for _, s := range spans {
		_, sp := tr.Start(ctx, s.Name, trace.WithTimestamp(at(s.Start)),
			trace.WithAttributes(append(s.Attrs, attribute.Int("node", s.Node))...))
		sp.End(trace.WithTimestamp(at(s.End)))
	}
	for _, e := range events {
		root.AddEvent(e.Name, trace.WithTimestamp(at(e.At)), trace.WithAttributes(attribute.Int("node", e.Node)))
	}
	root.End(trace.WithTimestamp(at(endNS)))
	return root.SpanContext().TraceID().String()
}
