package sim

import (
	"github.com/aceqbit/seedlog/check"
	"github.com/aceqbit/seedlog/core"
	"github.com/aceqbit/seedlog/kv"
)

type client struct {
	id          uint64
	seq         uint64
	cur         kv.Command
	op          int
	outstanding bool
	attempt     uint32
	hint        int
	known       [64]int64
}

func (s *Sim) setupClients() {
	s.clients = make([]client, s.cfg.Clients)
	for i := range s.clients {
		s.clients[i] = client{id: uint64(i + 1), hint: core.None}
		s.hp.push(event{at: s.now + int64(s.src.Draw(50*uint64(ms))), kind: evClientIssue, a: uint64(i)})
	}
}

func (s *Sim) pickOp() kv.OpKind {
	m := &s.cfg.Mix
	total := m[0] + m[1] + m[2] + m[3]
	r := s.src.Draw(total)
	switch {
	case r < m[0]:
		return kv.OpPut
	case r < m[0]+m[1]:
		return kv.OpGet
	case r < m[0]+m[1]+m[2]:
		return kv.OpCAS
	}
	return kv.OpIncr
}

func (s *Sim) clientIssue(ci int) {
	c := &s.clients[ci]
	c.seq++
	cmd := kv.Command{ClientID: c.id, Seq: c.seq, Op: s.pickOp(), Key: uint32(s.src.Draw(uint64(s.cfg.Keys)))}
	uniq := int64(c.id*1_000_000 + c.seq)
	switch cmd.Op {
	case kv.OpPut:
		cmd.Value = uniq
		s.cov(CovPut)
	case kv.OpGet:
		s.cov(CovGet)
	case kv.OpCAS:
		cmd.Value = uniq
		if s.src.Draw(10) < 7 {
			cmd.Expect = c.known[cmd.Key]
		} else {
			cmd.Expect = int64(s.src.Draw(5))
		}
	case kv.OpIncr:
		cmd.Value = 1 + int64(s.src.Draw(3))
		s.cov(CovIncr)
	}
	c.cur = cmd
	c.outstanding = true
	c.attempt = 0
	c.op = s.hist.Invoke(check.Op{ClientID: cmd.ClientID, Seq: cmd.Seq, Kind: cmd.Op, Key: cmd.Key,
		Value: cmd.Value, Expect: cmd.Expect, Call: s.now})
	s.sendAttempt(ci)
}

func (s *Sim) sendAttempt(ci int) {
	c := &s.clients[ci]
	c.attempt++
	if c.attempt > 1 {
		s.cov(CovClientRetry)
	}
	target := c.hint
	if target == core.None || c.attempt%3 == 0 {
		target = int(s.src.Draw(uint64(s.cfg.Nodes)))
	}
	if s.dropPm > 0 && s.src.Draw(1000) < s.dropPm {
		s.fault(FaultDrop)
		s.cov(CovDrop)
	} else {
		lat := 200_000 + int64(s.src.Draw(uint64(ms)))
		s.hp.push(event{at: s.now + lat, kind: evClientReq, node: int16(target), a: uint64(ci), b: c.cur.Seq})
	}
	s.hp.push(event{at: s.now + s.cfg.ClientTimeout, kind: evClientRetry, a: uint64(ci), b: c.cur.Seq, g: c.attempt})
}

func (s *Sim) clientRequest(node, ci int, seq uint64) {
	c := &s.clients[ci]
	if !c.outstanding || c.cur.Seq != seq || !s.nodes[node].alive {
		return
	}
	s.step(node, core.Event{Kind: core.EvClient, Cmd: c.cur})
}

func (s *Sim) clientRetry(ci int, seq uint64, attempt uint32) {
	c := &s.clients[ci]
	if !c.outstanding || c.cur.Seq != seq || c.attempt != attempt {
		return
	}
	s.sendAttempt(ci)
}

func (s *Sim) clientReply(r core.Reply) {
	ci := int(r.ClientID) - 1
	if ci < 0 || ci >= len(s.clients) {
		return
	}
	c := &s.clients[ci]
	if !c.outstanding || c.cur.Seq != r.Seq {
		return
	}
	if r.Status == core.ReplyNotLeader {
		c.hint = r.Leader
		s.hp.push(event{at: s.now + 5*ms + int64(s.src.Draw(uint64(15*ms))), kind: evClientRetry, a: uint64(ci), b: c.cur.Seq, g: c.attempt})
		return
	}
	c.hint = core.None
	c.outstanding = false
	s.hist.Complete(c.op, s.now, r.Result)
	c.known[c.cur.Key] = r.Result.Value
	if c.cur.Op == kv.OpCAS {
		if r.Result.OK {
			s.cov(CovCASOK)
		} else {
			s.cov(CovCASFail)
		}
	}
	if c.cur.Op.IsWrite() && r.Index > 0 {
		s.led.Ack(c.id, c.cur.Seq, r.Index, r.Term, c.cur.Hash())
	}
	s.hp.push(event{at: s.now + int64(s.src.Draw(uint64(s.cfg.ThinkMax))), kind: evClientIssue, a: uint64(ci)})
}
