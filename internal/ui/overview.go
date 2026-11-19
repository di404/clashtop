package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m *Model) overviewView(h int) string {
	w := m.width
	st := m.st

	type card struct {
		title, value string
		style        lipgloss.Style
		spark        []int64
		sub          string // shown instead of a sparkline
	}
	proxyPct := "-"
	if t := st.proxyBytes + st.directBytes; t > 0 {
		proxyPct = fmt.Sprintf("%.0f%%", float64(st.proxyBytes)*100/float64(t))
	}
	totUp, totDown := st.totals()
	cards := []card{
		{title: "下载", value: fmtSpeed(last(st.down)), style: sDown, spark: st.down},
		{title: "上传", value: fmtSpeed(last(st.up)), style: sUp, spark: st.up},
		{title: "总量", value: "↓ " + fmtBytes(totDown), style: sDown,
			sub: sUp.Render("↑ " + fmtBytes(totUp))},
		{title: "走代理", value: proxyPct, style: sText,
			sub: sMuted.Render("代理 " + fmtShort(last(st.proxySpeed)) + " 直连 " + fmtShort(last(st.directSpeed)))},
		{title: "活动连接", value: fmt.Sprintf("%d", len(st.active)), style: sText, spark: st.connCount},
		{title: "内存", value: fmtBytes(last(st.memory)), style: sText, spark: st.memory},
	}
	perRow := 6
	if w < 96 {
		perRow = 3
	}
	cw := (w - perRow) / perRow // width incl. border
	if cw < 14 {
		cw = 14
	}
	inner := cw - 4
	var cardRows []string
	for i := 0; i < len(cards); i += perRow {
		var row []string
		for _, c := range cards[i:min(i+perRow, len(cards))] {
			spark := strings.Repeat(" ", inner)
			if c.sub != "" {
				spark = fit(c.sub, inner, false)
			} else if c.spark != nil {
				spark = sparkline(c.spark, inner, c.style)
			}
			content := sBoxTitle.Render(fit(c.title, inner, false)) + "\n" +
				c.style.Bold(true).Render(fit(c.value, inner, false)) + "\n" + spark
			row = append(row, sBox.Width(cw-2).Render(content))
		}
		cardRows = append(cardRows, lipgloss.JoinHorizontal(lipgloss.Top, intersperse(row, " ")...))
	}
	top := strings.Join(cardRows, "\n")
	used := lipgloss.Height(top)

	// bottom: top hosts / processes by current speed
	listH := clamp((h-used)/3, 0, 12)
	bottom := ""
	if listH >= 3 {
		half := (w - 1) / 2
		bottom = lipgloss.JoinHorizontal(lipgloss.Top,
			m.topList("活跃主机", groupHost, half, listH),
			" ",
			m.topList("活跃进程", groupProcess, w-half-1, listH),
		)
	}
	chartH := h - used - lipgloss.Height(bottom) - 1
	charts := ""
	if chartH >= 4 {
		each := chartH / 2
		charts = m.trafficChart("下载", st.down, sDown, w, each) + "\n" +
			m.trafficChart("上传", st.up, sUp, w, chartH-each)
	}
	parts := []string{top}
	if charts != "" {
		parts = append(parts, charts)
	}
	if bottom != "" {
		parts = append(parts, bottom)
	}
	return strings.Join(parts, "\n")
}

func intersperse(xs []string, sep string) []string {
	out := make([]string, 0, len(xs)*2)
	for i, x := range xs {
		if i > 0 {
			out = append(out, sep)
		}
		out = append(out, x)
	}
	return out
}

// trafficChart draws a titled bar chart with a y-axis scale on the left.
func (m *Model) trafficChart(title string, data []int64, style lipgloss.Style, w, h int) string {
	const axisW = 11
	plotH := h - 1
	if plotH < 1 {
		return ""
	}
	plotW := w - axisW - 1
	rows, scale := barChart(data, plotW, plotH, style)
	var peak int64
	window := data
	if len(window) > plotW {
		window = window[len(window)-plotW:]
	}
	for _, v := range window {
		peak = max(peak, v)
	}
	head := style.Bold(true).Render(title) + sMuted.Render(fmt.Sprintf("  当前 %s  峰值 %s  %ds",
		fmtSpeed(last(data)), fmtSpeed(peak), plotW))
	var b strings.Builder
	b.WriteString(fit(head, w, false))
	for i, r := range rows {
		label := ""
		switch i {
		case 0:
			if scale > 0 {
				label = fmtBytes(scale)
			}
		case len(rows) - 1:
			label = "0"
		}
		b.WriteString("\n" + sFaint.Render(fit(label, axisW-1, true)+" │") + r)
	}
	return b.String()
}

func (m *Model) topList(title string, g groupKind, w, h int) string {
	var items []*agg
	for _, a := range m.st.groups[g] {
		if a.Active > 0 && a.UpSpeed+a.DownSpeed > 0 {
			items = append(items, a)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		si, sj := items[i].UpSpeed+items[i].DownSpeed, items[j].UpSpeed+items[j].DownSpeed
		if si != sj {
			return si > sj
		}
		return items[i].Key < items[j].Key
	})
	inner := w - 4
	numW := 11
	nameW := inner - numW*2
	var b strings.Builder
	b.WriteString(sHeader.Render(fit(title, nameW, false)) +
		sDown.Render(fit("↓", numW, true)) + sUp.Render(fit("↑", numW, true)))
	rows := h - 3
	for i := 0; i < rows; i++ {
		b.WriteString("\n")
		if i >= len(items) {
			continue
		}
		a := items[i]
		b.WriteString(sText.Render(fit(a.Key, nameW, false)) +
			sDown.Render(fit(fmtSpeed(a.DownSpeed), numW, true)) +
			sUp.Render(fit(fmtSpeed(a.UpSpeed), numW, true)))
	}
	if len(items) == 0 {
		return sBox.Width(w - 2).Render(sHeader.Render(title) + "\n" + sMuted.Render("暂无流量") +
			strings.Repeat("\n", max(0, rows-1)))
	}
	return sBox.Width(w - 2).Render(b.String())
}
