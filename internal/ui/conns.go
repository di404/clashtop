package ui

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	colHost = iota
	colProcess
	colRule
	colChain
	colDown
	colUp
	colDownTotal
	colUpTotal
	colTime
	colCount
)

var colTitles = []string{"主机", "进程", "规则", "链路", "↓速度", "↑速度", "↓总量", "↑总量", "时长"}

type connsView struct {
	cursor, offset int
	selID          string
	sortCol        int
	desc           bool
	filter         string
	showClosed     bool
	frozen         []*conn // non-nil while frozen
	detail         bool
}

func (v *connsView) move(n int) { v.cursor += n; v.selID = "" }

func (m *Model) connRows() []*conn {
	v := &m.conns
	var src []*conn
	switch {
	case v.frozen != nil:
		src = v.frozen
	case v.showClosed:
		src = make([]*conn, len(m.st.closed))
		for i, c := range m.st.closed {
			src[len(src)-1-i] = c // newest first
		}
	default:
		src = make([]*conn, 0, len(m.st.active))
		for _, c := range m.st.active {
			src = append(src, c)
		}
	}
	rows := src[:0:0]
	for _, c := range src {
		if v.filter == "" || connMatches(c, v.filter) {
			rows = append(rows, c)
		}
	}
	if v.showClosed && v.frozen == nil && v.sortCol == colTime {
		return rows // keep close order
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		less, eq := connLess(a, b, v.sortCol)
		if eq {
			if !a.Start.Equal(b.Start) {
				return a.Start.After(b.Start)
			}
			return a.ID < b.ID
		}
		if v.desc {
			return !less
		}
		return less
	})
	return rows
}

func connMatches(c *conn, f string) bool {
	for _, s := range []string{c.target(), c.process(), c.chain(), c.rule(), c.Metadata.DestinationIP,
		c.Metadata.SourceIP, c.Metadata.Network, c.Metadata.Type} {
		if containsFold(s, f) {
			return true
		}
	}
	return false
}

// connLess returns (a<b, a==b) for column col.
func connLess(a, b *conn, col int) (bool, bool) {
	cmpS := func(x, y string) (bool, bool) {
		return strings.ToLower(x) < strings.ToLower(y), strings.EqualFold(x, y)
	}
	cmpI := func(x, y int64) (bool, bool) { return x < y, x == y }
	switch col {
	case colHost:
		return cmpS(a.host(), b.host())
	case colProcess:
		return cmpS(a.process(), b.process())
	case colRule:
		return cmpS(a.rule(), b.rule())
	case colChain:
		return cmpS(a.chain(), b.chain())
	case colDown:
		return cmpI(a.DownSpeed, b.DownSpeed)
	case colUp:
		return cmpI(a.UpSpeed, b.UpSpeed)
	case colDownTotal:
		return cmpI(a.Download, b.Download)
	case colUpTotal:
		return cmpI(a.Upload, b.Upload)
	default: // duration: longer = "greater"
		return a.Start.After(b.Start), a.Start.Equal(b.Start)
	}
}

func (m *Model) connsKey(key string) tea.Cmd {
	v := &m.conns
	rows := m.connRows()
	page := max(1, m.height-4)
	switch key {
	case "up", "k":
		v.move(-1)
	case "down", "j":
		v.move(1)
	case "pgup", "ctrl+u":
		v.move(-page)
	case "pgdown", "ctrl+d":
		v.move(page)
	case "g", "home":
		v.cursor, v.selID = 0, ""
	case "G", "end":
		v.cursor, v.selID = len(rows)-1, ""
	case "enter":
		v.detail = !v.detail
	case "s":
		v.sortCol = (v.sortCol + 1) % colCount
		v.desc = v.sortCol >= colDown // numbers default to largest first
	case "S":
		v.desc = !v.desc
	case "left", "h", "right", "l":
		v.showClosed = !v.showClosed
		v.frozen = nil
		if v.showClosed {
			v.sortCol, v.desc = colTime, true // most recently closed first
		} else {
			v.sortCol, v.desc = colDown, true
		}
		v.cursor, v.offset, v.selID = 0, 0, ""
	case "f":
		if v.frozen != nil {
			v.frozen = nil
		} else {
			v.frozen = append([]*conn{}, rows...)
		}
	case "x":
		if c := m.selectedConn(rows); c != nil && c.ClosedAt.IsZero() {
			return m.s.closeConn(c.ID)
		}
	case "X":
		if len(m.st.active) > 0 {
			m.confirm = "closeAll"
		}
	}
	return nil
}

func (m *Model) selectedConn(rows []*conn) *conn {
	v := &m.conns
	if len(rows) == 0 {
		return nil
	}
	return rows[clamp(v.cursor, 0, len(rows)-1)]
}

type column struct {
	w     int
	right bool
}

func (m *Model) connColumns(w int) []column {
	cols := []column{
		colHost: {0, false}, colProcess: {14, false}, colRule: {18, false}, colChain: {22, false},
		colDown: {10, true}, colUp: {10, true}, colDownTotal: {9, true}, colUpTotal: {9, true}, colTime: {7, true},
	}
	// drop low-priority columns on narrow terminals
	if w < 130 {
		cols[colUpTotal].w = 0
		cols[colRule].w = 14
	}
	if w < 110 {
		cols[colDownTotal].w = 0
		cols[colChain].w = 16
	}
	if w < 90 {
		cols[colRule].w = 0
	}
	fixed := 0
	for i, c := range cols {
		if i != colHost && c.w > 0 {
			fixed += c.w + 1
		}
	}
	cols[colHost].w = max(12, w-fixed-2) // leading space + host's own separator
	return cols
}

func (m *Model) connsView(h int) string {
	v := &m.conns
	rows := m.connRows()
	w := m.width

	// keep the selection on the same connection across refreshes
	if v.selID != "" {
		for i, c := range rows {
			if c.ID == v.selID {
				v.cursor = i
				break
			}
		}
	}
	v.cursor = clamp(v.cursor, 0, max(0, len(rows)-1))
	if sel := m.selectedConn(rows); sel != nil {
		v.selID = sel.ID
	}

	detailH := 0
	var detail string
	if v.detail {
		if c := m.selectedConn(rows); c != nil {
			detail = m.connDetail(c, w)
			detailH = strings.Count(detail, "\n") + 1
		}
	}

	cols := m.connColumns(w)
	var hdr strings.Builder
	hdr.WriteString(" ")
	for i, c := range cols {
		if c.w == 0 {
			continue
		}
		t := colTitles[i]
		if i == v.sortCol {
			if v.desc {
				t += "▼"
			} else {
				t += "▲"
			}
		}
		hdr.WriteString(fit(t, c.w, c.right) + " ")
	}

	listH := h - 1 - detailH
	if listH < 1 {
		listH = 1
	}
	if v.cursor < v.offset {
		v.offset = v.cursor
	}
	if v.cursor >= v.offset+listH {
		v.offset = v.cursor - listH + 1
	}
	v.offset = clamp(v.offset, 0, max(0, len(rows)-listH))

	var b strings.Builder
	b.WriteString(sHeader.Render(fit(hdr.String(), w, false)))
	now := time.Now()
	for i := v.offset; i < len(rows) && i < v.offset+listH; i++ {
		b.WriteString("\n" + m.connLine(rows[i], cols, w, i == v.cursor, now))
	}
	if len(rows) == 0 {
		b.WriteString("\n" + sMuted.Render(" 无连接"))
	}
	out := b.String()
	if detail != "" {
		pad := listH + 1 - (strings.Count(out, "\n") + 1)
		out += strings.Repeat("\n", max(0, pad)) + "\n" + detail
	}
	return out
}

func (m *Model) connLine(c *conn, cols []column, w int, sel bool, now time.Time) string {
	end := now
	if !c.ClosedAt.IsZero() {
		end = c.ClosedAt
	}
	cells := []string{
		colHost:      c.target(),
		colProcess:   c.process(),
		colRule:      c.rule(),
		colChain:     c.chain(),
		colDown:      fmtSpeed(c.DownSpeed),
		colUp:        fmtSpeed(c.UpSpeed),
		colDownTotal: fmtBytes(c.Download),
		colUpTotal:   fmtBytes(c.Upload),
		colTime:      fmtDur(end.Sub(c.Start)),
	}
	var b strings.Builder
	b.WriteString(" ")
	for i, col := range cols {
		if col.w == 0 {
			continue
		}
		s := fit(cells[i], col.w, col.right)
		if !sel {
			switch i {
			case colDown, colDownTotal:
				if c.DownSpeed > 0 || i == colDownTotal {
					s = sDown.Render(s)
				} else {
					s = sFaint.Render(s)
				}
			case colUp, colUpTotal:
				if c.UpSpeed > 0 || i == colUpTotal {
					s = sUp.Render(s)
				} else {
					s = sFaint.Render(s)
				}
			case colProcess, colRule, colChain, colTime:
				s = sMuted.Render(s)
			default:
				s = sText.Render(s)
			}
		}
		b.WriteString(s + " ")
	}
	line := fit(b.String(), w, false)
	if sel {
		return sSel.Bold(true).Render(line)
	}
	if c.stun() { // counters are bogus; show it, but visibly discounted
		return sFaint.Render(ansi.Strip(line))
	}
	return line
}

func (m *Model) connDetail(c *conn, w int) string {
	md := c.Metadata
	kv := [][2]string{
		{"目标", c.target()},
		{"远端", md.RemoteDestination},
		{"来源", md.SourceIP + ":" + md.SourcePort + "  " + md.InboundName},
		{"类型", md.Network + " / " + md.Type + "  dns:" + md.DNSMode},
		{"进程", md.ProcessPath},
		{"规则", c.rule()},
		{"链路", c.chain()},
		{"流量", "↓ " + fmtBytes(c.Download) + "   ↑ " + fmtBytes(c.Upload)},
		{"开始", c.Start.Local().Format("2006-01-02 15:04:05")},
	}
	var b strings.Builder
	inner := w - 4
	first := true
	for _, p := range kv {
		if p[1] == "" || strings.TrimSpace(p[1]) == ":" {
			continue
		}
		if !first {
			b.WriteString("\n")
		}
		first = false
		b.WriteString(sMuted.Render("  "+p[0]+"  ") + sText.Render(fit(p[1], inner-8, false)))
	}
	return sFaint.Render(strings.Repeat("─", w)) + "\n" + b.String()
}

func itoa(n int) string { return strconv.Itoa(n) }
