// Package check holds the checkers both hosts share: online Raft invariants
// over core Observations, a Porcupine linearizability adapter partitioned by
// key, a brute-force oracle for tiny histories, and the acknowledged-write
// ledger. A checker reports the first Violation it sees; a linearizability
// timeout is Unknown, never a pass.
package check
