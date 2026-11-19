package ui

import (
	"time"

	"github.com/di404/clashtop/internal/api"
)

const (
	historyLen = 600
	maxClosed  = 1000
	maxLogs    = 5000
)

// conn is a connection plus derived per-second speeds.
type conn struct {
	api.Connection
	UpSpeed   int64
	DownSpeed int64
	ClosedAt  time.Time
}

func (c *conn) host() string    { return c.Host() }
func (c *conn) process() string { return c.Process() }
func (c *conn) node() string    { return c.Node() }
func (c *conn) stun() bool      { return c.STUN() }
func (c *conn) direct() bool    { return c.Direct() }

func (c *conn) target() string {
	h := c.host()
	if c.Metadata.DestinationPort != "" {
		h += ":" + c.Metadata.DestinationPort
	}
	return h
}

func (c *conn) chain() string {
	s := ""
	for i := len(c.Chains) - 1; i >= 0; i-- {
		if s != "" {
			s += " › "
		}
		s += c.Chains[i]
	}
	if s == "" {
		return "-"
	}
	return s
}

func (c *conn) rule() string { return c.RuleText() }

// Grouping dimensions for the stats view.
type groupKind int

const (
	groupProcess groupKind = iota
	groupHost
	groupNode
	groupRule
	groupCount
)

var groupNames = []string{"进程", "主机", "节点", "规则"}

func groupKey(c *conn, g groupKind) string {
	switch g {
	case groupProcess:
		return c.process()
	case groupHost:
		return c.host()
	case groupNode:
		return c.node()
	default:
		return c.rule()
	}
}

type agg struct {
	Key       string
	Conns     int
	Active    int
	Up        int64
	Down      int64
	UpSpeed   int64
	DownSpeed int64
}

type store struct {
	version string

	up, down           []int64 // per-second history
	upTotal, downTotal int64
	memory             []int64
	connCount          []int64

	active    map[string]*conn
	closed    []*conn // oldest first
	lastPoll  time.Time
	firstPoll bool

	groups [groupCount]map[string]*agg

	// proxy vs direct split, from per-connection deltas (STUN excluded)
	proxySpeed, directSpeed []int64 // down+up per second
	proxyBytes, directBytes int64   // since start

	// While a STUN connection exists (or just closed), mihomo's global /traffic
	// numbers may carry its bogus counters; use the clean per-connection sum instead.
	stunActive         bool
	cleanUp, cleanDown int64
	// STUN bytes seen (full counters of STUN conns open at startup, deltas after),
	// subtracted from the kernel totals for display
	stunUp, stunDown int64
	trafficSeq       int // /traffic samples received; history index = seq-(total-len)
	pollSeq          int // trafficSeq at the previous poll

	logs []api.Log
}

func newStore() *store {
	s := &store{active: map[string]*conn{}, firstPoll: true}
	for i := range s.groups {
		s.groups[i] = map[string]*agg{}
	}
	return s
}

func push(h []int64, v int64) []int64 {
	h = append(h, v)
	if len(h) > historyLen {
		h = h[len(h)-historyLen:]
	}
	return h
}

func last(h []int64) int64 {
	if len(h) == 0 {
		return 0
	}
	return h[len(h)-1]
}

func (s *store) addTraffic(t api.Traffic) {
	if s.stunActive {
		t.Up, t.Down = s.cleanUp, s.cleanDown
	}
	s.up = push(s.up, t.Up)
	s.down = push(s.down, t.Down)
	s.trafficSeq++
	if t.UpTotal > 0 || t.DownTotal > 0 {
		s.upTotal, s.downTotal = t.UpTotal, t.DownTotal
	}
}

// totals returns mihomo's cumulative traffic minus the STUN bytes seen.
func (s *store) totals() (up, down int64) {
	return max(0, s.upTotal-s.stunUp), max(0, s.downTotal-s.stunDown)
}

func (s *store) addMemory(m api.Memory) {
	if m.Inuse == 0 && len(s.memory) == 0 {
		return // mihomo sends an initial zero sample
	}
	s.memory = push(s.memory, m.Inuse)
}

// applyConnections diffs a snapshot against the previous one: computes speeds,
// moves vanished connections to the closed list, and accumulates group stats.
// Traffic of connections already open at startup is only counted from startup on.
func (s *store) applyConnections(snap api.Connections, now time.Time) {
	dt := now.Sub(s.lastPoll).Seconds()
	if s.lastPoll.IsZero() || dt <= 0 {
		dt = 1
	}
	s.lastPoll = now
	if snap.UploadTotal > 0 || snap.DownloadTotal > 0 {
		s.upTotal, s.downTotal = snap.UploadTotal, snap.DownloadTotal
	}
	for g := range s.groups {
		for _, a := range s.groups[g] {
			a.UpSpeed, a.DownSpeed, a.Active = 0, 0, 0
		}
	}

	var proxyNow, directNow, cleanUp, cleanDown int64
	stunActive := false
	seen := make(map[string]bool, len(snap.Connections))
	for _, ac := range snap.Connections {
		seen[ac.ID] = true
		prev, ok := s.active[ac.ID]
		var dUp, dDown int64
		c := &conn{Connection: ac}
		switch {
		case ok:
			dUp, dDown = ac.Upload-prev.Upload, ac.Download-prev.Download
		case !s.firstPoll:
			dUp, dDown = ac.Upload, ac.Download
		}
		if dUp < 0 {
			dUp = 0
		}
		if dDown < 0 {
			dDown = 0
		}
		if c.stun() {
			if s.firstPoll {
				dUp, dDown = ac.Upload, ac.Download
			}
			s.stunUp += dUp
			s.stunDown += dDown
			stunActive = true
			dUp, dDown = 0, 0
		}
		if !s.firstPoll {
			c.UpSpeed = int64(float64(dUp) / dt)
			c.DownSpeed = int64(float64(dDown) / dt)
		}
		s.active[ac.ID] = c
		cleanUp += c.UpSpeed
		cleanDown += c.DownSpeed
		if c.direct() {
			s.directBytes += dUp + dDown
			directNow += c.UpSpeed + c.DownSpeed
		} else {
			s.proxyBytes += dUp + dDown
			proxyNow += c.UpSpeed + c.DownSpeed
		}
		if c.stun() {
			continue // bogus counters: keep out of the stats
		}
		for g := range s.groups {
			k := groupKey(c, groupKind(g))
			a := s.groups[g][k]
			if a == nil {
				a = &agg{Key: k}
				s.groups[g][k] = a
			}
			if !ok {
				a.Conns++
			}
			a.Active++
			a.Up += dUp
			a.Down += dDown
			a.UpSpeed += c.UpSpeed
			a.DownSpeed += c.DownSpeed
		}
	}
	for id, c := range s.active {
		if seen[id] {
			continue
		}
		delete(s.active, id)
		if c.stun() { // it may have pushed bytes into /traffic before closing
			stunActive = true
		}
		c.UpSpeed, c.DownSpeed = 0, 0
		c.ClosedAt = now
		s.closed = append(s.closed, c)
	}
	if len(s.closed) > maxClosed {
		s.closed = s.closed[len(s.closed)-maxClosed:]
	}
	s.stunActive, s.cleanUp, s.cleanDown = stunActive, cleanUp, cleanDown
	if stunActive { // every /traffic sample since the last poll is polluted
		for i := max(0, len(s.up)-(s.trafficSeq-s.pollSeq)); i < len(s.up); i++ {
			s.up[i], s.down[i] = cleanUp, cleanDown
		}
	}
	s.pollSeq = s.trafficSeq
	s.connCount = push(s.connCount, int64(len(s.active)))
	if !s.firstPoll {
		s.proxySpeed = push(s.proxySpeed, proxyNow)
		s.directSpeed = push(s.directSpeed, directNow)
	}
	s.firstPoll = false
}

func (s *store) addLogs(ls []api.Log) {
	s.logs = append(s.logs, ls...)
	if len(s.logs) > maxLogs {
		s.logs = append([]api.Log(nil), s.logs[len(s.logs)-maxLogs:]...)
	}
}
