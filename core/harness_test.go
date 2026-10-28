package core

import "github.com/aceqbit/seedlog/kv"

// cluster is a synchronous test host: messages are queued and delivered in
// FIFO order, persistence completes when the test says so.
type cluster struct {
	nodes   []*Node
	queue   []Message
	issued  []uint64 // last token issued per node
	batches [][]PersistBatch
	replies []Reply
	down    []bool
}

func testConfig(id, n int, bugs Bugs) Config {
	return Config{ID: id, N: n, ElectionBase: 150, ElectionJitter: 150, Heartbeat: 50, MaxBatch: 4, Bugs: bugs}
}

func newCluster(n int, bugs Bugs) *cluster {
	c := &cluster{issued: make([]uint64, n), batches: make([][]PersistBatch, n), down: make([]bool, n)}
	for i := 0; i < n; i++ {
		c.nodes = append(c.nodes, NewNode(testConfig(i, n, bugs), PersistentState{Vote: None}))
		c.step(i, Event{Kind: EvStart})
	}
	return c
}

func (c *cluster) step(i int, ev Event) {
	if c.down[i] {
		return
	}
	effs, _ := c.nodes[i].Step(ev)
	for _, e := range effs {
		switch e.Kind {
		case EffSend:
			c.queue = append(c.queue, e.Msg)
		case EffPersist:
			c.issued[i] = e.Token
			c.batches[i] = append(c.batches[i], e.Batch)
		case EffReply:
			c.replies = append(c.replies, e.Reply)
		}
	}
}

func (c *cluster) persistAll() {
	for i := range c.nodes {
		if c.issued[i] > c.nodes[i].done {
			c.step(i, Event{Kind: EvPersistDone, Token: c.issued[i]})
		}
	}
}

// settle persists and delivers until nothing is left.
func (c *cluster) settle() {
	for steps := 0; steps < 10000; steps++ {
		c.persistAll()
		if len(c.queue) == 0 {
			return
		}
		m := c.queue[0]
		c.queue = c.queue[1:]
		c.step(m.To, Event{Kind: EvMessage, Msg: m})
	}
	panic("cluster did not settle")
}

func (c *cluster) elect(i int) {
	c.step(i, Event{Kind: EvTimer, Timer: TimerElection})
	c.settle()
}

func (c *cluster) heartbeat(i int) {
	c.step(i, Event{Kind: EvTimer, Timer: TimerHeartbeat})
	c.settle()
}

func (c *cluster) client(i int, cmd kv.Command) {
	c.step(i, Event{Kind: EvClient, Cmd: cmd})
	c.settle()
}

func (c *cluster) leaders(term uint64) []int {
	var ls []int
	for i, n := range c.nodes {
		if n.Role() == Leader && n.Term() == term {
			ls = append(ls, i)
		}
	}
	return ls
}

// restart replaces node i with one recovered from its persisted batches.
func (c *cluster) restart(i int) {
	var img []byte
	for k := range c.batches[i] {
		img = EncodeBatch(img, &c.batches[i][k])
	}
	st, _ := Recover(img, false)
	c.nodes[i] = NewNode(c.nodes[i].cfg, st)
	c.issued[i] = 0
	c.down[i] = false
	c.step(i, Event{Kind: EvStart})
}
