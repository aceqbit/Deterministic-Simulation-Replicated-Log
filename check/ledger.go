package check

import "fmt"

type ack struct {
	index, term, hash uint64
	client, seq       uint64
}

// Ledger records every acknowledged write and verifies at the end of a run
// that each one is still in the log of a node that committed past it.
type Ledger struct {
	acks []ack
}

// Ack records that the write (client, seq) was acknowledged as committed at
// (index, term) with command hash hash.
func (l *Ledger) Ack(client, seq, index, term, hash uint64) {
	l.acks = append(l.acks, ack{index, term, hash, client, seq})
}

// Len returns the number of recorded acknowledgements.
func (l *Ledger) Len() int { return len(l.acks) }

// Reset empties the ledger, keeping storage.
func (l *Ledger) Reset() { l.acks = l.acks[:0] }

// Check verifies every acknowledged write against the nodes mirrored by o.
// An ack is lost when some node committed past its index and no such node
// holds the acknowledged entry there.
func (l *Ledger) Check(o *Online, at int64) *Violation {
	for _, a := range l.acks {
		committedPast, held := false, false
		for n := range o.logs {
			if o.nodeCommit[n] < a.index {
				continue
			}
			committedPast = true
			if r, ok := o.nodeEntry(n, a.index); ok && r == (rec{a.term, a.hash}) {
				held = true
				break
			}
		}
		if committedPast && !held {
			return &Violation{Detector: DetAckLedger, Invariant: "acked_write_lost", Time: at, Node: -1,
				Detail: fmt.Sprintf("write client %d seq %d acked at index %d term %d is gone", a.client, a.seq, a.index, a.term)}
		}
	}
	return nil
}
