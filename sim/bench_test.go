package sim

import "testing"

// BenchmarkSeed runs one reference-profile seed per iteration and reports
// events and simulated seconds so make bench can derive the targets.
func BenchmarkSeed(b *testing.B) {
	cfg := Reference()
	cfg.CheckLin = false
	b.ReportAllocs()
	var events uint64
	for i := 0; i < b.N; i++ {
		r := run(uint64(i%50)+1, cfg)
		events += r.Events
	}
	b.ReportMetric(float64(events)/float64(b.N), "events/seed")
	b.ReportMetric(60*float64(b.N)/b.Elapsed().Seconds(), "sim-s/s")
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(max(events, 1)), "ns/event")
}
