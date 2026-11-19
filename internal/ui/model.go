package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/di404/clashtop/internal/api"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type tab int

const (
	tabOverview tab = iota
	tabConns
	tabLogs
	tabStats
)

var tabNames = []string{"概览", "连接", "日志", "统计"}

var logLevels = []string{"debug", "info", "warning", "error"}

type Options struct {
	Interval time.Duration
	LogLevel string
	UsageDir string // where the daemon writes per-day usage
	Notice   string // shown once at startup
}

type Model struct {
	st    *store
	s     *streams
	label string

	width, height int
	tab           tab
	errs          map[string]error
	flash         string
	flashAt       time.Time
	help          bool
	confirm       string // pending confirmation: "closeAll"

	// text input for the filter of the current tab
	inputOn bool

	conns connsView
	logs  logsView
	stats statsView

	usageDir string
	hist     *history
}

type tickMsg time.Time

// Run starts the TUI and blocks until the user quits.
func Run(c *api.Client, opt Options) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if opt.Interval <= 0 {
		opt.Interval = time.Second
	}
	if opt.LogLevel == "" {
		opt.LogLevel = "info"
	}
	m := &Model{
		st:       newStore(),
		s:        newStreams(ctx, c, opt.Interval),
		label:    c.Label,
		errs:     map[string]error{},
		conns:    connsView{sortCol: colDown, desc: true},
		logs:     logsView{level: opt.LogLevel, wrap: true},
		stats:    statsView{sortCol: 0},
		usageDir: opt.UsageDir,
	}
	if opt.Notice != "" {
		m.flash, m.flashAt = opt.Notice, time.Now().Add(5*time.Second)
	}
	detectPalette()
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	m.s.send = p.Send
	m.s.start(opt.LogLevel)
	_, err := p.Run()
	return err
}

func (m *Model) Init() tea.Cmd { return tick() }

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *Model) setFlash(s string) {
	m.flash = s
	m.flashAt = time.Now()
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = max(0, msg.Width-2*padX), max(0, msg.Height-2*padY)
	case tickMsg:
		if m.flash != "" && time.Since(m.flashAt) > 3*time.Second {
			m.flash = ""
		}
		if m.tab == tabStats {
			m.loadHistory(false)
		}
		return m, tick()
	case trafficMsg:
		m.st.addTraffic(api.Traffic(msg))
	case memoryMsg:
		m.st.addMemory(api.Memory(msg))
	case connsMsg:
		m.st.applyConnections(msg.snap, msg.at)
	case logsMsg:
		m.st.addLogs(msg)
		if m.logs.offset > 0 { // keep the viewport anchored while scrolled up
			m.logs.offset += len(m.logs.filtered(msg))
		}
	case versionMsg:
		m.st.version = string(msg)
	case streamErr:
		if msg.err == nil {
			delete(m.errs, msg.src)
		} else {
			m.errs[msg.src] = msg.err
		}
	case actionMsg:
		m.setFlash(msg.text)
	case tea.MouseMsg:
		return m, m.handleMouse(msg)
	case tea.KeyMsg:
		return m, m.handleKey(msg)
	}
	return m, nil
}

func (m *Model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.scroll(-3)
	case tea.MouseButtonWheelDown:
		m.scroll(3)
	}
	return nil
}

func (m *Model) scroll(n int) {
	switch m.tab {
	case tabConns:
		m.conns.move(n)
	case tabLogs:
		m.logs.scroll(-n)
	case tabStats:
		m.stats.move(n)
	}
}

func (m *Model) currentFilter() *string {
	switch m.tab {
	case tabConns:
		return &m.conns.filter
	case tabLogs:
		return &m.logs.filter
	case tabStats:
		return &m.stats.filter
	}
	return nil
}

func (m *Model) handleKey(k tea.KeyMsg) tea.Cmd {
	key := k.String()
	if key == "ctrl+c" {
		return tea.Quit
	}

	if m.inputOn {
		f := m.currentFilter()
		switch k.Type {
		case tea.KeyEnter:
			m.inputOn = false
		case tea.KeyEsc:
			m.inputOn = false
			*f = ""
		case tea.KeyBackspace:
			r := []rune(*f)
			if len(r) > 0 {
				*f = string(r[:len(r)-1])
			}
		case tea.KeyRunes, tea.KeySpace:
			*f += string(k.Runes)
		}
		return nil
	}

	if m.confirm != "" {
		c := m.confirm
		m.confirm = ""
		if key == "y" && c == "closeAll" {
			return m.s.closeConn("")
		}
		m.setFlash("已取消")
		return nil
	}

	if m.help {
		m.help = false
		return nil
	}

	switch key {
	case "q":
		return tea.Quit
	case "?":
		m.help = true
		return nil
	case "1", "2", "3", "4":
		m.tab = tab(key[0] - '1')
		return nil
	case "tab":
		m.tab = (m.tab + 1) % 4
		return nil
	case "shift+tab":
		m.tab = (m.tab + 3) % 4
		return nil
	case "/":
		if m.currentFilter() != nil {
			m.inputOn = true
		}
		return nil
	case "esc":
		if f := m.currentFilter(); f != nil {
			*f = ""
		}
		m.conns.detail = false
		return nil
	}

	switch m.tab {
	case tabConns:
		return m.connsKey(key)
	case tabLogs:
		return m.logsKey(key)
	case tabStats:
		m.statsKey(key)
	}
	return nil
}

// ---------- layout ----------

// macOS 窗口圆角会裁掉四角，内容向内缩进。
const (
	padX = 2
	padY = 0
)

func (m *Model) View() string {
	if m.width == 0 {
		return ""
	}
	bodyH := m.height - 2
	if bodyH < 1 {
		bodyH = 1
	}
	var body string
	if m.help {
		body = m.helpView(bodyH)
	} else {
		switch m.tab {
		case tabOverview:
			body = m.overviewView(bodyH)
		case tabConns:
			body = m.connsView(bodyH)
		case tabLogs:
			body = m.logsView(bodyH)
		case tabStats:
			body = m.statsView(bodyH)
		}
	}
	body = lipgloss.NewStyle().Height(bodyH).MaxHeight(bodyH).Render(body)
	out := lipgloss.JoinVertical(lipgloss.Left, m.headerView(), body, m.footerView())
	return lipgloss.NewStyle().Padding(padY, padX).Render(out)
}

func (m *Model) headerView() string {
	left := sTitle.Render("ClashTop") + "   " + m.tabsView()

	var status string
	if len(m.errs) > 0 {
		srcs := make([]string, 0, len(m.errs))
		for s := range m.errs {
			srcs = append(srcs, s)
		}
		sort.Strings(srcs)
		status = sErr.Render("● " + srcs[0] + ": " + m.errs[srcs[0]].Error())
	} else if m.st.version != "" {
		status = sOK.Render("●")
	} else {
		status = sMuted.Render("○ 连接中")
	}

	right := sDown.Render("↓ "+fmtSpeed(last(m.st.down))) + "  " +
		sUp.Render("↑ "+fmtSpeed(last(m.st.up))) + "  " +
		sText.Render(fmt.Sprintf("%d 连接", len(m.st.active)))
	if mem := last(m.st.memory); mem > 0 {
		right += "  " + sMuted.Render(fmtBytes(mem))
	}
	right += "  " + status

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		right = fit(right, max(0, m.width-lipgloss.Width(left)-1), false)
		gap = 1
	}
	return fit(left+strings.Repeat(" ", gap)+right, m.width, false)
}

func (m *Model) tabsView() string {
	parts := make([]string, len(tabNames))
	for i, n := range tabNames {
		st := sTabOff
		if tab(i) == m.tab && !m.help {
			st = sTabOn
		}
		parts[i] = sFaint.Render(itoa(i+1)) + " " + st.Render(n)
	}
	return strings.Join(parts, "  ")
}

// keyVal renders a key with the current value of what it switches, so the
// footer shows both the state and how to change it.
func keyVal(key, val string, st lipgloss.Style) string {
	return sKey.Render(key) + " " + st.Render(val)
}

func keyHint(pairs ...string) string {
	var b []string
	for i := 0; i+1 < len(pairs); i += 2 {
		b = append(b, sKey.Render(pairs[i])+" "+sMuted.Render(pairs[i+1]))
	}
	return strings.Join(b, "  ")
}

func (m *Model) footerView() string {
	if m.inputOn {
		return sKey.Render("/") + m.inputBufView()
	}
	if m.confirm == "closeAll" {
		return sWarn.Render(fmt.Sprintf("关闭全部 %d 个连接？y 确认，其他键取消", len(m.st.active)))
	}
	var hint string
	switch m.tab {
	case tabOverview:
		hint = keyHint("1-4", "切换", "?", "帮助", "q", "退出")
	case tabConns:
		mode := keyVal("←→", "活动", sText)
		if m.conns.showClosed {
			mode = keyVal("←→", "已关闭", sWarn.Bold(true))
		}
		freeze := keyHint("f", "冻结")
		if m.conns.frozen != nil {
			freeze = keyVal("f", "已冻结", sWarn.Bold(true))
		}
		hint = mode + "  " + keyHint("↑↓", "选择", "enter", "详情", "s/S", "排序", "/", "过滤") + "  " + freeze +
			"  " + keyHint("x", "断开", "X", "全部断开")
	case tabLogs:
		follow := keyHint("G", "跟随")
		if m.logs.offset > 0 {
			follow = keyVal("G", "已暂停 ↑"+itoa(m.logs.offset), sWarn.Bold(true))
		}
		hint = keyVal("←→", m.logs.level, levelStyle(m.logs.level).Bold(true)) + "  " +
			keyHint("↑↓", "滚动") + "  " + follow + "  " + keyHint("/", "过滤", "w", "换行", "C", "清空")
	case tabStats:
		hint = keyHint("←→", "维度") + "  " + keyVal("t", statSpans[m.stats.span], sText.Bold(true)) + "  " +
			keyHint("↑↓", "选择", "s", "排序", "/", "过滤", "r", "重置")
	}
	if f := m.currentFilter(); f != nil && *f != "" {
		hint = sKey.Render("过滤: ") + sText.Render(*f) + "  " + hint
	}
	if m.flash != "" {
		hint = sWarn.Render(m.flash) + "  " + hint
	}
	return fit(hint, m.width, false)
}

func (m *Model) inputBufView() string {
	f := m.currentFilter()
	if f == nil {
		return ""
	}
	return sText.Render(*f) + sKey.Render("▏")
}

func (m *Model) helpView(h int) string {
	rows := [][2]string{
		{"1 2 3 4 / tab", "切换页面"},
		{"/", "过滤当前页（回车确认，esc 清除）"},
		{"q / ctrl+c", "退出"},
		{"", ""},
		{"连接页", ""},
		{"↑↓ j k pgup pgdn g G", "移动"},
		{"enter", "展开 / 收起详情"},
		{"s / S", "切换排序列 / 反转顺序"},
		{"← → h l", "活动连接 ↔ 已关闭连接（底栏显示当前是哪个）"},
		{"f", "冻结列表，便于查看"},
		{"x / X", "断开所选连接 / 断开全部（需确认）"},
		{"", ""},
		{"日志页", ""},
		{"↑↓ pgup pgdn", "滚动，G 回到底部跟随"},
		{"← → h l", "切换订阅级别 debug / info / warning / error"},
		{"w", "长行换行"},
		{"C", "清空已收日志"},
		{"", ""},
		{"统计页", ""},
		{"← → h l", "切换维度：进程 / 主机 / 节点 / 规则"},
		{"t", "切换范围：本次运行 / 今天 / 7 天 / 30 天"},
		{"s", "切换排序"},
		{"r", "重置本次运行的累计数据"},
	}
	var b strings.Builder
	about := "mihomo"
	if m.st.version != "" {
		about = m.st.version
	}
	b.WriteString(sMuted.Render(about) + "  " + sFaint.Render(m.label) + "\n\n")
	for _, r := range rows {
		if r[1] == "" {
			b.WriteString(sTitle.Render(r[0]) + "\n")
			continue
		}
		b.WriteString("  " + sKey.Render(fit(r[0], 24, false)) + sText.Render(r[1]) + "\n")
	}
	b.WriteString("\n" + sMuted.Render("「本次」只算本次运行；今天 / 7 天 / 30 天来自后台记录（clashtop daemon）。"))
	return b.String()
}
