// Package kv is the replicated state machine: a key/value store over int64
// registers with client sessions so that a retried command applies once.
//
// It is pure: no clock, randomness, I/O or goroutines.
package kv
