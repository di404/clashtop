package ui

import (
	"sort"
	"strings"
	"time"

	"github.com/di404/clashtop/internal/usage"
)

type statsView struct {
	group          groupKind
	span           int // index into statSpans
	cursor, offset int
	sortCol        int // 0 total, 1 down, 2 up, 3 speed, 4 conns, 5 name
	filter         string
}

const statSortCount = 6 // total, down, up, speed, conns, name

// statSpans: 0 = this run (in memory), else the last N days from the daemon.
var (
	statSpans    = []string{"本次", "今天", "7 天", "30 天"}
	statSpanDays = []int{0, 1, 7, 30}
)

// history caches the daemon's files for the selected span.
type history struct {
	span     int
	day      *usage.Day
	found    bool
	loadedAt time.Time
}

const histRefresh = 15 * time.Second // the daemon flushes about this often

func (m *Model) loadHistory(force bool) {
	span := m.stats.span
	if span == 0 || m.usageDir == "" {
		return
	}
	if !force && m.hist != nil && m.hist.span == span && time.Since(m.hist.loadedAt) < histRefresh {
		return
	}
	d, found := usage.Load(m.usageDir, statSpanDays[span], time.Now())
	m.hist = &history{span: span, day: d, found: found, loadedAt: time.Now()}
}

func (v *statsView) move(n int) { v.cursor += n }

func (m *Model) statsKey(key string) {
	v := &m.stats
	page := max(1, m.height-4)
	switch key {
	case "left", "h":
		v.group = (v.group + groupCount - 1) % groupCount
		v.cursor, v.offset = 0, 0
	case "right", "l":
		v.group = (v.group + 1) % groupCount
		v.cursor, v.offset = 0, 0
	case "up", "k":
		v.move(-1)
	case "down", "j":
		v.move(1)
	case "pgup", "ctrl+u":
		v.move(-page)
	case "pgdown", "ctrl+d":
		v.move(page)
	case "g", "home":
		v.cursor = 0
	case "G", "end":
		v.cursor = 1 << 30
	case "s":
		v.sortCol = (v.sortCol + 1) % statSortCount
	case "t":
		v.span = (v.span + 1) % len(statSpans)
		v.cursor, v.offset = 0, 0
		m.loadHistory(true)
	case "r":
		if v.span != 0 {
			m.setFlash("历史记录不在这里重置")
			return
		}
		for g := range m.st.groups {
			m.st.groups[g] = map[string]*agg{}
		}
		m.setFlash("统计已重置")
	}
}

func (m *Model) statRows() ([]*agg, int64) {
	v := &m.stats
	var rows []*agg
	var total int64
	src := m.st.groups[v.group]
	if v.span != 0 && m.hist != nil {
		// history totals, with live speed / active count from this run
		src = map[string]*agg{}
		for k, e := range m.hist.day.Group(int(v.group)) {
			a := &agg{Key: k, Conns: e.Conns, Up: e.Up, Down: e.Down}
			if live := m.st.groups[v.group][k]; live != nil {
				a.Active, a.UpSpeed, a.DownSpeed = live.Active, live.UpSpeed, live.DownSpeed
			}
			src[k] = a
		}
	}
	for _, a := range src {
		total += a.Up + a.Down
		if v.filter == "" || containsFold(a.Key, v.filter) {
			rows = append(rows, a)
		}
	}
	key := func(a *agg) int64 {
		switch v.sortCol {
		case 1:
			return a.Down
		case 2:
			return a.Up
		case 3:
			return a.UpSpeed + a.DownSpeed
		case 4:
			return int64(a.Conns)
		}
		return a.Up + a.Down
	}
	sort.Slice(rows, func(i, j int) bool {
		if v.sortCol == 5 {
			return strings.ToLower(rows[i].Key) < strings.ToLower(rows[j].Key)
		}
		ki, kj := key(rows[i]), key(rows[j])
		if ki != kj {
			return ki > kj
		}
		return rows[i].Key < rows[j].Key
	})
	return rows, total
}

func (m *Model) statsView(h int) string {
	v := &m.stats
	w := m.width
	rows, total := m.statRows()

	const numW = 10
	barW := clamp(w/5, 8, 30)
	nameW := max(12, w-barW-60) // see line layout below: name + bar + 60 fixed cells
	// header titles carry the sort marker, so the sort needs no separate label
	col := func(t string, sorts ...int) string {
		for _, s := range sorts {
			if s == v.sortCol {
				if s == 5 {
					return t + "▲"
				}
				return t + "▼"
			}
		}
		return t
	}
	// the name column is labelled with the dimension itself; ←→ switches it
	hdr := " " + sTitle.Render(fit(col("‹ "+groupNames[v.group]+" ›", 5), nameW, false)) + sHeader.Render(" "+fit(col("连接", 4), 6, true)+" "+
		fit(col("↓总量", 1), numW, true)+" "+fit(col("↑总量", 2), numW, true)+" "+
		fit(col("↓速度", 3), numW, true)+" "+fit(col("↑速度", 3), numW, true)+"  "+fit(col("占比", 0)+"  共 "+fmtBytes(total), barW+6, false))

	listH := max(1, h-1)
	v.cursor = clamp(v.cursor, 0, max(0, len(rows)-1))
	if v.cursor < v.offset {
		v.offset = v.cursor
	}
	if v.cursor >= v.offset+listH {
		v.offset = v.cursor - listH + 1
	}

	var b strings.Builder
	b.WriteString(fit(hdr, w, false))
	for i := v.offset; i < len(rows) && i < v.offset+listH; i++ {
		a := rows[i]
		share := 0.0
		if total > 0 {
			share = float64(a.Up+a.Down) / float64(total)
		}
		filled := int(share*float64(barW) + 0.5)
		bar := strings.Repeat("━", filled)
		rest := strings.Repeat("─", barW-filled)
		pct := fit(itoa(int(share*100+0.5))+"%", 5, true)
		sel := i == v.cursor
		name := fit(a.Key, nameW, false)
		cells := []string{
			fit(itoa(a.Conns), 6, true),
			fit(fmtBytes(a.Down), numW, true),
			fit(fmtBytes(a.Up), numW, true),
			fit(fmtSpeed(a.DownSpeed), numW, true),
			fit(fmtSpeed(a.UpSpeed), numW, true),
		}
		var line string
		if sel {
			line = sSel.Bold(true).Render(fit(" "+name+" "+strings.Join(cells, " ")+"  "+bar+rest+" "+pct, w, false))
		} else {
			line = " " + sText.Render(name) + " " + sMuted.Render(cells[0]) + " " +
				sDown.Render(cells[1]) + " " + sUp.Render(cells[2]) + " " +
				speedStyle(a.DownSpeed, sDown).Render(cells[3]) + " " + speedStyle(a.UpSpeed, sUp).Render(cells[4]) + "  " +
				sTitle.UnsetBold().Render(bar) + sFaint.Render(rest) + " " + sMuted.Render(pct)
		}
		b.WriteString("\n" + line)
	}
	if len(rows) == 0 {
		msg := " 暂无数据"
		if v.span != 0 && (m.hist == nil || !m.hist.found) {
			msg = " 没有后台记录，可运行 clashtop daemon install"
		}
		b.WriteString("\n" + sMuted.Render(msg))
	}
	return b.String()
}

func speedStyle(v int64, s interface{ Render(...string) string }) interface{ Render(...string) string } {
	if v > 0 {
		return s
	}
	return sFaint
}
