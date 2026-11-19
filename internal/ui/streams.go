package ui

import (
	"context"
	"sync"
	"time"

	"github.com/di404/clashtop/internal/api"

	tea "github.com/charmbracelet/bubbletea"
)

type (
	trafficMsg api.Traffic
	memoryMsg  api.Memory
	connsMsg   struct {
		snap api.Connections
		at   time.Time
	}
	logsMsg    []api.Log
	versionMsg string
	streamErr  struct {
		src string
		err error // nil = recovered
	}
	actionMsg struct{ text string }
)

// streams owns the background goroutines feeding the UI.
type streams struct {
	client   *api.Client
	interval time.Duration
	send     func(tea.Msg)

	ctx       context.Context
	logMu     sync.Mutex
	logCancel context.CancelFunc
}

func newStreams(ctx context.Context, c *api.Client, interval time.Duration) *streams {
	return &streams{client: c, interval: interval, ctx: ctx}
}

// retry runs fn until ctx ends, reporting failures and recoveries under src.
func (s *streams) retry(ctx context.Context, src string, fn func(ctx context.Context, ok func()) error) {
	failed := false
	ok := func() {
		if failed {
			failed = false
			s.send(streamErr{src: src})
		}
	}
	for ctx.Err() == nil {
		err := fn(ctx, ok)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			failed = true
			s.send(streamErr{src: src, err: err})
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func (s *streams) start(logLevel string) {
	go s.retry(s.ctx, "version", func(ctx context.Context, ok func()) error {
		v, err := s.client.Version(ctx)
		if err != nil {
			return err
		}
		ok()
		name := "Clash"
		if v.Meta {
			name = "mihomo"
		}
		s.send(versionMsg(name + " " + v.Version))
		<-ctx.Done()
		return nil
	})
	go s.retry(s.ctx, "traffic", func(ctx context.Context, ok func()) error {
		return s.client.StreamTraffic(ctx, func(t api.Traffic) { ok(); s.send(trafficMsg(t)) })
	})
	go s.retry(s.ctx, "memory", func(ctx context.Context, ok func()) error {
		return s.client.StreamMemory(ctx, func(m api.Memory) { ok(); s.send(memoryMsg(m)) })
	})
	go s.retry(s.ctx, "connections", func(ctx context.Context, ok func()) error {
		t := time.NewTicker(s.interval)
		defer t.Stop()
		for {
			snap, err := s.client.Connections(ctx)
			if err != nil {
				return err
			}
			ok()
			s.send(connsMsg{snap: snap, at: time.Now()})
			select {
			case <-ctx.Done():
				return nil
			case <-t.C:
			}
		}
	})
	s.setLogLevel(logLevel)
}

// setLogLevel (re)subscribes to /logs at the given level. Logs are batched
// so a busy debug stream doesn't trigger a repaint per line.
func (s *streams) setLogLevel(level string) {
	s.logMu.Lock()
	if s.logCancel != nil {
		s.logCancel()
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.logCancel = cancel
	s.logMu.Unlock()

	go s.retry(ctx, "logs", func(ctx context.Context, ok func()) error {
		var mu sync.Mutex
		var buf []api.Log
		done := make(chan struct{})
		defer close(done)
		go func() {
			t := time.NewTicker(150 * time.Millisecond)
			defer t.Stop()
			for {
				select {
				case <-done:
					return
				case <-t.C:
					mu.Lock()
					b := buf
					buf = nil
					mu.Unlock()
					if len(b) > 0 {
						s.send(logsMsg(b))
					}
				}
			}
		}()
		return s.client.StreamLogs(ctx, level, func(l api.Log) {
			ok()
			mu.Lock()
			buf = append(buf, l)
			mu.Unlock()
		})
	})
}

func (s *streams) closeConn(id string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
		defer cancel()
		if err := s.client.CloseConnection(ctx, id); err != nil {
			return actionMsg{"关闭失败: " + err.Error()}
		}
		if id == "" {
			return actionMsg{"已关闭全部连接"}
		}
		return actionMsg{"已关闭连接"}
	}
}
