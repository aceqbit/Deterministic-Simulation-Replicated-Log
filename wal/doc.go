// Package wal frames write-ahead-log records as [len u32 | crc32c u32 | payload]
// using the Castagnoli table, and recovers a byte image to its last valid
// record. Storage is the interface both hosts implement: the simulator with a
// disk model, the runtime with a file.
//
// It is pure: no clock, randomness, I/O or goroutines.
package wal
