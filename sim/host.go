package sim

import (
	"github.com/aceqbit/seedlog/check"
	"github.com/aceqbit/seedlog/core"
)

func (s *Sim) allocMsg(m core.Message) int32 {
	if n := len(s.freeMsg); n > 0 {
		i := s.freeMsg[n-1]
		s.freeMsg = s.freeMsg[:n-1]
		s.msgs[i] = m
		return i
	}
	s.msgs = append(s.msgs, m)
	return int32(len(s.msgs) - 1)
}

func (s *Sim) allocRep(r core.Reply) int32 {
	if n := len(s.freeRep); n > 0 {
		i := s.freeRep[n-1]
		s.freeRep = s.freeRep[:n-1]
		s.reps[i] = r
		return i
	}
	s.reps = append(s.reps, r)
	return int32(len(s.reps) - 1)
}

func (s *Sim) fault(k int) { s.res.Faults[k]++ }

func (s *Sim) cov(p int) { s.res.Cov[p]++ }

// step feeds one event to node i, checks its observations and executes its effects.
func (s *Sim) step(i int, ev core.Event) {
	effs, obs := s.nodes[i].n.Step(ev)
	for k := range obs {
		s.observe(i, &obs[k])
	}
	s.on.EndStep(i)
	for k := range effs {
		s.apply(i, &effs[k])
	}
}

func (s *Sim) observe(i int, o *core.Observation) {
	if s.OnObserve != nil {
		s.OnObserve(i, s.now, *o)
	}
	s.th.u64(uint64(o.Kind)<<32 | uint64(uint32(o.Node)))
	s.th.u64(o.Term)
	s.th.u64(o.Index)
	s.th.u64(o.A)
	s.th.u64(o.B)
	s.on.Observe(s.now, *o)
	sn := &s.nodes[i]
	switch o.Kind {
	case core.ObsElectionStart:
		s.cov(CovElection)
		if s.sawCrash {
			s.cov(CovElectionAfterCrash)
			s.sawCrash = false
		}
		if s.roles[i] == core.Candidate {
			s.cov(CovSplitVote)
		}
	case core.ObsState:
		s.roles[i] = core.Role(o.A)
		if core.Role(o.A) == core.Leader && s.lastLeader != i {
			s.targetedCrash(i, 3, uint64(80*ms))
			s.cov(CovLeaderElected)
			if s.lastLeader != core.None {
				s.cov(CovLeaderChange)
			}
			s.res.LeaderChanges++
			s.lastLeader = i
			s.afterChange = true
		}
	case core.ObsStepDown:
		if core.Role(o.A) == core.Leader {
			s.cov(CovLeaderStepDown)
		} else {
			s.cov(CovCandidateStepDown)
		}
	case core.ObsTruncate:
		s.cov(CovLogTruncation)
	case core.ObsCommit:
		if s.roles[i] == core.Leader {
			s.cov(CovCommit)
			if s.afterChange {
				s.cov(CovCommitAfterLeaderChange)
				s.afterChange = false
			}
		}
	case core.ObsVoteGranted:
		s.cov(CovVoteGranted)
		s.targetedCrash(i, 4, uint64(ms))
	case core.ObsVoteRejectedLog:
		s.cov(CovVoteRejectedLog)
	case core.ObsVoteRejectedVoted:
		s.cov(CovVoteRejectedVoted)
	case core.ObsAppendReject:
		if o.A == 1 {
			s.cov(CovAppendRejectMissing)
		} else {
			s.cov(CovAppendRejectConflict)
		}
	case core.ObsStaleTerm:
		s.cov(CovStaleTerm)
	case core.ObsBatch:
		s.cov(CovBatch)
	case core.ObsDedupHit:
		s.cov(CovDedupHit)
	case core.ObsNotLeader:
		s.cov(CovNotLeader)
	case core.ObsGated:
		s.cov(CovGated)
	case core.ObsHeartbeat:
		s.cov(CovHeartbeat)
	case core.ObsRestore:
		if o.Index > 0 {
			s.cov(CovRestoreNonEmpty)
		} else {
			s.cov(CovRestoreEmpty)
		}
		sn.recovered = o.Index
	case core.ObsAppend:
		if sn.catchUp && o.Index > sn.recovered {
			s.cov(CovRestartCatchUp)
			sn.catchUp = false
		}
	}
}

func (s *Sim) apply(i int, e *core.Effect) {
	switch e.Kind {
	case core.EffSend:
		s.send(i, e.Msg)
	case core.EffSetTimer:
		sn := &s.nodes[i]
		d := e.Base
		if e.Jitter > 0 {
			d += int64(s.src.Draw(uint64(e.Jitter) + 1))
		}
		d += d * sn.skew / 1_000_000
		sn.timerGen[e.Timer]++
		s.hp.push(event{at: s.now + d, kind: evTimer, node: int16(i), a: uint64(e.Timer), b: sn.timerGen[e.Timer], g: sn.gen})
	case core.EffPersist:
		s.persist(i, e)
	case core.EffReply:
		if s.dropPm > 0 && s.src.Draw(1000) < s.dropPm {
			s.fault(FaultDrop)
			s.cov(CovDrop)
			return
		}
		lat := 200_000 + int64(s.src.Draw(uint64(ms)))
		s.hp.push(event{at: s.now + lat, kind: evClientReply, m: s.allocRep(e.Reply)})
	}
}

func (s *Sim) send(from int, m core.Message) {
	to := m.To
	if s.blocked[from][to] {
		return
	}
	if s.dropPm > 0 && s.src.Draw(1000) < s.dropPm {
		s.fault(FaultDrop)
		s.cov(CovDrop)
		return
	}
	l := s.lat[from][to]
	d := l.base + int64(s.src.Draw(uint64(l.jit)))
	if s.reorderPm > 0 && s.src.Draw(1000) < s.reorderPm {
		d += int64(s.src.Draw(uint64(50 * ms)))
		s.fault(FaultReorder)
		s.cov(CovReorder)
	}
	if s.Hooks.Message != nil {
		s.Hooks.Message(from, to, m.Type, s.now, s.now+d)
	}
	s.hp.push(event{at: s.now + d, kind: evMsg, node: int16(to), m: s.allocMsg(m)})
	if s.dupPm > 0 && s.src.Draw(1000) < s.dupPm {
		d2 := l.base + int64(s.src.Draw(uint64(l.jit))) + int64(s.src.Draw(uint64(5*ms)))
		s.fault(FaultDup)
		s.cov(CovDup)
		s.hp.push(event{at: s.now + d2, kind: evMsg, node: int16(to), m: s.allocMsg(m)})
	}
}

func (s *Sim) persist(i int, e *core.Effect) {
	sn := &s.nodes[i]
	s.scratch = core.EncodeBatch(s.scratch[:0], &e.Batch)
	id := sn.disk.append(s.scratch)
	lat := 50_000 + int64(s.src.Draw(uint64(3*ms/2)))
	if s.src.Draw(64) == 0 {
		lat += int64(s.src.Draw(uint64(30 * ms)))
	}
	done := max(s.now+lat, sn.syncDone)
	sn.syncDone = done
	if s.Hooks.Fsync != nil {
		s.Hooks.Fsync(i, s.now, done)
	}
	s.hp.push(event{at: done, kind: evSync, node: int16(i), a: e.Token, b: id, g: sn.gen})
}

func (s *Sim) downCount() int {
	c := 0
	for i := range s.nodes {
		if !s.nodes[i].alive {
			c++
		}
	}
	return c
}

func (s *Sim) crash(i int) {
	sn := &s.nodes[i]
	if !sn.alive {
		return
	}
	if s.roles[i] == core.Leader {
		s.cov(CovCrashLeader)
	}
	sn.alive = false
	sn.gen++
	dropped, torn := sn.disk.crash(s.src, s.enabled(FaultLostFsync), s.enabled(FaultTorn))
	for k := 0; k < dropped; k++ {
		s.fault(FaultLostFsync)
	}
	if dropped > 0 {
		s.cov(CovLostWrite)
	}
	for k := 0; k < torn; k++ {
		s.fault(FaultTorn)
	}
	if torn > 0 {
		s.cov(CovTornWrite)
	}
	s.fault(FaultCrash)
	s.cov(CovCrash)
	if s.Hooks.Fault != nil {
		s.Hooks.Fault("crash", i, s.now)
		for k := 0; k < torn; k++ {
			s.Hooks.Fault("torn_write", i, s.now)
		}
	}
	s.sawCrash = true
	if d := s.downCount(); d >= 2 {
		s.cov(CovCrashTwoDown)
		if d >= s.cfg.Nodes/2+1 {
			s.cov(CovCrashMajorityDown)
		}
	}
	s.roles[i] = core.Follower
	if s.lastLeader == i {
		s.lastLeader = core.None
	}
	// A supervisor restarts some crashed processes almost at once.
	delay := 100*ms + int64(s.src.Draw(uint64(3*sec/2)))
	if s.src.Draw(2) == 0 {
		delay = int64(s.src.Draw(uint64(10 * ms)))
	}
	s.hp.push(event{at: s.now + delay, kind: evRestart, node: int16(i), g: sn.gen})
}

func (s *Sim) restart(i int) {
	sn := &s.nodes[i]
	if sn.alive {
		return
	}
	st, info := core.Recover(sn.disk.durable, s.cfg.Bugs.AcceptTornTail)
	if info.TornAccepted {
		s.fail(check.DetWALRecovery, "checksum_failing_record_accepted", i, "recovery accepted a record with a bad checksum")
	}
	if info.Corrupt {
		s.fail(check.DetWALRecovery, "recovered_corrupt_state", i, "recovery produced an impossible log: %s", info.Reason)
	}
	if info.Truncated > 0 {
		s.cov(CovTornTailRecovered)
	}
	sn.disk.truncate(info.Valid)
	sn.alive = true
	sn.catchUp = true
	sn.n = core.NewNode(s.coreConfig(i), st)
	s.step(i, core.Event{Kind: core.EvStart})
}

func (s *Sim) nemesis() {
	var acts [8]int
	na := 0
	add := func(a int) { acts[na] = a; na++ }
	if s.enabled(FaultCrash) {
		add(0)
		add(5) // crash the current leader
	}
	if s.enabled(FaultPause) {
		add(1)
	}
	if s.enabled(FaultPartition) {
		add(2)
		add(3)
		add(4)
		add(6) // cut the current leader off from everyone
	}
	if na == 0 {
		return
	}
	n := s.cfg.Nodes
	switch acts[s.src.Draw(uint64(na))] {
	case 0:
		s.crash(int(s.src.Draw(uint64(n))))
	case 1:
		i := int(s.src.Draw(uint64(n)))
		dur := 100*ms + int64(s.src.Draw(uint64(sec)))
		sn := &s.nodes[i]
		sn.pausedUntil = max(sn.pausedUntil, s.now+dur)
		s.fault(FaultPause)
		s.cov(CovPause)
		if s.roles[i] == core.Leader {
			s.cov(CovPausedLeader)
		}
	case 2:
		side := s.src.Draw(uint64(1)<<uint(n)-2) + 1
		for a := 0; a < n; a++ {
			for b := 0; b < n; b++ {
				if (side>>uint(a))&1 != (side>>uint(b))&1 {
					s.blocked[a][b] = true
				}
			}
		}
		s.fault(FaultPartition)
		s.cov(CovPartitionSym)
		if s.Hooks.Fault != nil {
			s.Hooks.Fault("partition", -1, s.now)
		}
	case 3:
		a := int(s.src.Draw(uint64(n)))
		b := int(s.src.Draw(uint64(n - 1)))
		if b >= a {
			b++
		}
		s.blocked[a][b] = true
		s.fault(FaultPartition)
		s.cov(CovPartitionAsym)
		if s.Hooks.Fault != nil {
			s.Hooks.Fault("partition", a, s.now)
		}
	case 4:
		s.healNet()
	case 5:
		if s.lastLeader != core.None {
			s.crash(s.lastLeader)
		}
	case 6:
		if l := s.lastLeader; l != core.None {
			for b := 0; b < n; b++ {
				if b != l {
					s.blocked[l][b], s.blocked[b][l] = true, true
				}
			}
			s.fault(FaultPartition)
			s.cov(CovPartitionSym)
		}
	}
}

func (s *Sim) healNet() {
	healed := false
	for a := range s.blocked {
		for b := range s.blocked[a] {
			if s.blocked[a][b] {
				healed = true
			}
			s.blocked[a][b] = false
		}
	}
	if healed {
		s.cov(CovPartitionHeal)
	}
}

func (s *Sim) heal() {
	s.healed = true
	s.healNet()
	s.dropPm, s.dupPm, s.reorderPm = 0, 0, 0
	for i := range s.nodes {
		if !s.nodes[i].alive {
			s.hp.push(event{at: s.now + 100*ms, kind: evRestart, node: int16(i), g: s.nodes[i].gen})
		}
	}
}

func (s *Sim) paused(e *event) bool {
	switch e.kind {
	case evMsg, evTimer, evSync, evClientReq:
		sn := &s.nodes[e.node]
		if s.now < sn.pausedUntil {
			e.at = sn.pausedUntil
			s.hp.push(*e)
			return true
		}
	}
	return false
}

func (s *Sim) dispatch(e *event) {
	i := int(e.node)
	switch e.kind {
	case evMsg:
		m := s.msgs[e.m]
		s.freeMsg = append(s.freeMsg, e.m)
		if !s.nodes[i].alive || s.blocked[m.From][m.To] {
			return
		}
		s.th.u64(uint64(m.Type)<<48 | uint64(m.From)<<32 | uint64(m.To))
		s.th.u64(m.Term)
		s.step(i, core.Event{Kind: core.EvMessage, Msg: m})
	case evTimer:
		sn := &s.nodes[i]
		if !sn.alive || sn.gen != e.g || sn.timerGen[e.a] != e.b {
			return
		}
		s.step(i, core.Event{Kind: core.EvTimer, Timer: core.TimerKind(e.a)})
	case evSync:
		sn := &s.nodes[i]
		if !sn.alive || sn.gen != e.g {
			return
		}
		sn.disk.syncUpTo(e.b)
		s.step(i, core.Event{Kind: core.EvPersistDone, Token: e.a})
	case evClientIssue:
		s.clientIssue(int(e.a))
	case evClientReq:
		s.clientRequest(i, int(e.a), e.b)
	case evClientReply:
		r := s.reps[e.m]
		s.freeRep = append(s.freeRep, e.m)
		s.clientReply(r)
	case evClientRetry:
		s.clientRetry(int(e.a), e.b, e.g)
	case evNemesis:
		if !s.healed {
			s.nemesis()
			s.hp.push(event{at: s.nextNemesis(), kind: evNemesis})
		}
	case evRestart:
		if s.nodes[i].gen == e.g {
			s.restart(i)
		}
	case evCrashNode:
		s.crash(i)
	case evHeal:
		s.heal()
	case evCheck:
		if v := s.led.Check(s.on, s.now); v != nil {
			s.on.Report(v)
		}
		s.hp.push(event{at: s.now + s.cfg.Duration/60, kind: evCheck})
	}
}

// Run executes the simulation to its end or first violation.
func (s *Sim) Run() (res Result) {
	defer func() {
		if r := recover(); r != nil {
			s.fail(check.DetPanic, "panic", -1, "%v", r)
			s.finish(&res)
		}
	}()
	s.th.reset()
	s.setup()
	for s.hp.len() > 0 && s.viol == nil {
		e := s.hp.pop()
		if e.at > s.cfg.Duration {
			break
		}
		s.now = e.at
		if s.paused(&e) {
			continue
		}
		s.res.Events++
		if s.cfg.MaxEvents > 0 && s.res.Events > s.cfg.MaxEvents {
			s.res.Budget = true
			break
		}
		s.th.u64(uint64(e.at))
		s.th.u64(uint64(e.kind)<<32 | uint64(uint16(e.node)))
		s.th.u64(e.a)
		s.dispatch(&e)
		if v := s.on.Violation(); v != nil && s.viol == nil {
			s.viol = v
		}
	}
	if s.viol == nil && !s.res.Budget {
		s.now = s.cfg.Duration
		s.endChecks()
	}
	s.finish(&res)
	return res
}

func (s *Sim) endChecks() {
	if v := s.led.Check(s.on, s.now); v != nil {
		s.viol = v
		return
	}
	leader := false
	for i := range s.nodes {
		if s.nodes[i].alive && s.nodes[i].n.Role() == core.Leader {
			leader = true
		}
	}
	if !s.cfg.NoFaults {
		if v := check.Liveness(&s.hist, s.healAt, s.cfg.LivenessBudget, s.now, leader); v != nil {
			s.viol = v
			return
		}
	}
	if s.cfg.CheckLin {
		s.res.LinChecked = true
		s.res.Lin = check.LinearizableMS(s.hist.Ops, s.cfg.LinTimeoutMS)
		if s.res.Lin == check.LinIllegal {
			s.fail(check.DetLinearizability, "history_not_linearizable", -1, "no valid linearization of %d operations", len(s.hist.Ops))
		}
	}
}

func (s *Sim) finish(res *Result) {
	s.res.Hash = s.th.sum()
	s.res.Draws = s.src.Count()
	s.res.VirtualNS = s.now
	s.res.Violation = s.viol
	s.res.Leader = core.None
	for i := range s.nodes {
		n := s.nodes[i].n
		if s.nodes[i].alive && n.Role() == core.Leader && (s.res.Leader == core.None || n.Term() > s.res.Term) {
			s.res.Leader, s.res.Term = i, n.Term()
		}
	}
	s.res.Ops = len(s.hist.Ops)
	s.res.Completed = s.hist.Completed()
	if s.cfg.RecordDraws {
		s.res.Choices = append([]uint64(nil), s.src.Log()...)
	}
	s.res.History = s.hist.Ops
	*res = s.res
}

// targetedCrash biases the nemesis toward the moments protocol bugs need: a
// crash right after a vote is granted or a leader is elected. It fires with
// probability 1/oneIn when crashes are enabled, within maxDelay.
func (s *Sim) targetedCrash(i int, oneIn, maxDelay uint64) {
	if !s.enabled(FaultCrash) || s.healed || s.src.Draw(oneIn) != 0 {
		return
	}
	s.hp.push(event{at: s.now + int64(s.src.Draw(maxDelay)), kind: evCrashNode, node: int16(i)})
}
