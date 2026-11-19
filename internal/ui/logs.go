package ui

import (
	"regexp"
	"strings"

	"github.com/di404/clashtop/internal/api"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type logsView struct {
	level  string
	filter string
	offset int // entries scrolled up from the bottom; 0 = follow
	wrap   bool
}

func levelRank(l string) int {
	switch l {
	case "debug":
		return 0
	case "info":
		return 1
	case "warning", "warn":
		return 2
	case "error":
		return 3
	}
	return 1
}

func (v *logsView) match(l api.Log) bool {
	if levelRank(l.Level) < levelRank(v.level) {
		return false
	}
	return v.filter == "" || containsFold(l.Message, v.filter) || containsFold(l.Level, v.filter)
}

func (v *logsView) filtered(ls []api.Log) []api.Log {
	out := ls[:0:0]
	for _, l := range ls {
		if v.match(l) {
			out = append(out, l)
		}
	}
	return out
}

func (v *logsView) scroll(n int) {
	v.offset = max(0, v.offset+n)
}

func (m *Model) logsKey(key string) tea.Cmd {
	v := &m.logs
	page := max(1, m.height-4)
	switch key {
	case "up", "k":
		v.scroll(1)
	case "down", "j":
		v.scroll(-1)
	case "pgup", "ctrl+u":
		v.scroll(page)
	case "pgdown", "ctrl+d":
		v.scroll(-page)
	case "g", "home":
		v.offset = len(m.st.logs)
	case "G", "end":
		v.offset = 0
	case "w":
		v.wrap = !v.wrap
	case "C":
		m.st.logs = nil
		v.offset = 0
	case "left", "h", "right", "l":
		step := 1
		if key == "left" || key == "h" {
			step = len(logLevels) - 1
		}
		i := 0
		for j, l := range logLevels {
			if l == v.level {
				i = j
			}
		}
		v.level = logLevels[(i+step)%len(logLevels)]
		m.s.setLogLevel(v.level)
	}
	return nil
}

var tagRe = regexp.MustCompile(`^\[[A-Za-z0-9 _-]+\]`)

func (m *Model) logLines(l api.Log, w int) []string {
	prefix := sFaint.Render(l.Time.Format("15:04:05")) + " " +
		levelStyle(l.Level).Render(fit(strings.ToUpper(shortLevel(l.Level)), 4, false)) + " "
	const prefixW = 14
	msg := l.Message
	tag := ""
	if t := tagRe.FindString(msg); t != "" {
		tag, msg = t, msg[len(t):]
	}
	body := sTitle.UnsetBold().Render(tag) + sText.Render(msg)
	avail := max(10, w-prefixW)
	if !m.logs.wrap {
		return []string{prefix + ansi.Truncate(body, avail, "…")}
	}
	wrapped := strings.Split(ansi.Hardwrap(body, avail, false), "\n")
	out := make([]string, len(wrapped))
	for i, s := range wrapped {
		if i == 0 {
			out[i] = prefix + s
		} else {
			out[i] = strings.Repeat(" ", prefixW) + s
		}
	}
	return out
}

func shortLevel(l string) string {
	switch l {
	case "warning":
		return "warn"
	case "error":
		return "err"
	}
	return l
}

func (m *Model) logsView(h int) string {
	v := &m.logs
	w := m.width
	entries := v.filtered(m.st.logs)
	v.offset = clamp(v.offset, 0, max(0, len(entries)-1))

	listH := h
	var lines []string
	for i := len(entries) - 1 - v.offset; i >= 0 && len(lines) < listH; i-- {
		ls := m.logLines(entries[i], w)
		lines = append(ls, lines...)
	}
	if len(lines) > listH {
		lines = lines[len(lines)-listH:]
	}
	if len(entries) == 0 {
		lines = []string{sMuted.Render("等待日志…")}
	}
	return strings.Join(lines, "\n")
}
