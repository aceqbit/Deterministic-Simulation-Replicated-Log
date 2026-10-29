// Package telemetry installs the OpenTelemetry SDK (OTLP/HTTP metrics and
// traces) and holds the SeedLog instruments. It observes runs and never
// steers them: with OTEL_EXPORTER_OTLP_ENDPOINT unset Setup does nothing and
// every instrument is a no-op. Simulator workers keep plain counters and call
// Explorer.Record once per seed.
package telemetry
