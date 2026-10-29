// Package runtime is the production host for the pure core: UDP transport with
// protobuf wire messages, a file Storage with fsync, real timers with jitter
// drawn from a per-node PRNG, one event-loop goroutine per node, and an HTTP
// client API. It is the only package besides cmd that starts goroutines.
package runtime
