// Package core is the Raft state machine shared by the simulator and the
// runtime. A Node consumes one Event per Step and returns the Effects the host
// must execute and Observations the host may record.
//
// The core never reads a clock, draws randomness, performs I/O or starts a
// goroutine. Time arrives as timer Events, election jitter is requested through
// SetTimer effects and drawn by the host, and durability is a Persist effect
// acknowledged by a PersistDone event. Every outgoing message and client reply
// is held until the state it depends on is durable.
package core
