package sim

type evKind uint8

const (
	evMsg evKind = iota + 1
	evTimer
	evSync
	evClientIssue
	evClientReq
	evClientReply
	evClientRetry
	evNemesis
	evRestart
	evHeal
	evCheck
	evCrashNode
)

// event is one scheduled occurrence, ordered by (at, seq).
type event struct {
	at   int64
	seq  uint64
	a, b uint64
	m    int32
	g    uint32
	node int16
	kind evKind
}

// heap is a min-heap of events keyed (at, seq). It is hand-rolled to avoid
// interface boxing.
type heap struct {
	ev  []event
	seq uint64
}

func less(a, b *event) bool { return a.at < b.at || (a.at == b.at && a.seq < b.seq) }

func (h *heap) push(e event) {
	h.seq++
	e.seq = h.seq
	h.ev = append(h.ev, e)
	i := len(h.ev) - 1
	for i > 0 {
		p := (i - 1) / 2
		if !less(&h.ev[i], &h.ev[p]) {
			break
		}
		h.ev[i], h.ev[p] = h.ev[p], h.ev[i]
		i = p
	}
}

func (h *heap) pop() event {
	top := h.ev[0]
	n := len(h.ev) - 1
	h.ev[0] = h.ev[n]
	h.ev = h.ev[:n]
	i := 0
	for {
		l, r, m := 2*i+1, 2*i+2, i
		if l < n && less(&h.ev[l], &h.ev[m]) {
			m = l
		}
		if r < n && less(&h.ev[r], &h.ev[m]) {
			m = r
		}
		if m == i {
			break
		}
		h.ev[i], h.ev[m] = h.ev[m], h.ev[i]
		i = m
	}
	return top
}

func (h *heap) len() int { return len(h.ev) }
