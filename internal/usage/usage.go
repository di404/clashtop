// Package usage records per-day traffic by process / host / node / rule to
// disk, so totals survive across TUI sessions. Only the daemon writes; the TUI
// reads.
package usage

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/di404/clashtop/internal/api"
)

// Dimensions, in the same order as the TUI's stats tabs.
const (
	Process = iota
	Host
	Node
	Rule
	Dims
)

type Entry struct {
	Up    int64 `json:"up"`
	Down  int64 `json:"down"`
	Conns int   `json:"conns"`
}

type Day struct {
	Date    string            `json:"date"` // 2006-01-02, local time
	Process map[string]*Entry `json:"process"`
	Host    map[string]*Entry `json:"host"`
	Node    map[string]*Entry `json:"node"`
	Rule    map[string]*Entry `json:"rule"`
}

func newDay(date string) *Day {
	return &Day{Date: date, Process: map[string]*Entry{}, Host: map[string]*Entry{},
		Node: map[string]*Entry{}, Rule: map[string]*Entry{}}
}

func (d *Day) Group(dim int) map[string]*Entry {
	switch dim {
	case Process:
		return d.Process
	case Host:
		return d.Host
	case Node:
		return d.Node
	default:
		return d.Rule
	}
}

func key(c *api.Connection, dim int) string {
	switch dim {
	case Process:
		return c.Process()
	case Host:
		return c.Host()
	case Node:
		return c.Node()
	default:
		return c.RuleText()
	}
}

// Dir is where usage data lives (~/Library/Application Support/clashtop on macOS).
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "clashtop"), nil
}

func dayPath(dir, date string) string { return filepath.Join(dir, "usage", date+".json") }

func loadDay(dir, date string) (*Day, error) {
	b, err := os.ReadFile(dayPath(dir, date))
	if errors.Is(err, fs.ErrNotExist) {
		return newDay(date), nil
	}
	if err != nil {
		return nil, err
	}
	d := newDay(date)
	if err := json.Unmarshal(b, d); err != nil {
		return nil, err
	}
	for dim := range Dims { // tolerate files missing a dimension
		if d.Group(dim) == nil {
			*d = *mergeInto(newDay(date), d)
			break
		}
	}
	return d, nil
}

func mergeInto(dst, src *Day) *Day {
	for dim := range Dims {
		for k, e := range src.Group(dim) {
			t := dst.Group(dim)[k]
			if t == nil {
				t = &Entry{}
				dst.Group(dim)[k] = t
			}
			t.Up += e.Up
			t.Down += e.Down
			t.Conns += e.Conns
		}
	}
	return dst
}

// writeJSON writes atomically so a reader never sees a half-written file.
func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Load merges the last `days` days (today included) ending at now.
// It reports whether any day file existed.
func Load(dir string, days int, now time.Time) (*Day, bool) {
	out := newDay("")
	found := false
	for i := range days {
		date := now.AddDate(0, 0, -i).Format(time.DateOnly)
		if _, err := os.Stat(dayPath(dir, date)); err != nil {
			continue
		}
		d, err := loadDay(dir, date)
		if err != nil {
			continue
		}
		found = true
		mergeInto(out, d)
	}
	return out, found
}

// state is the recorder's view of open connections, persisted so a daemon
// restart neither loses nor double-counts their bytes.
type state struct {
	SavedAt time.Time           `json:"savedAt"`
	Open    map[string][2]int64 `json:"open"` // id → upload, download
}

// Recorder turns /connections snapshots into per-day totals.
type Recorder struct {
	dir   string
	day   *Day
	prev  state
	first bool
	dirty bool
}

func NewRecorder(dir string, now time.Time) (*Recorder, error) {
	d, err := loadDay(dir, now.Format(time.DateOnly))
	if err != nil {
		return nil, err
	}
	r := &Recorder{dir: dir, day: d, first: true, prev: state{Open: map[string][2]int64{}}}
	if b, err := os.ReadFile(filepath.Join(dir, "state.json")); err == nil {
		_ = json.Unmarshal(b, &r.prev)
		if r.prev.Open == nil {
			r.prev.Open = map[string][2]int64{}
		}
	}
	return r, nil
}

// Apply records one snapshot taken at now.
func (r *Recorder) Apply(snap api.Connections, now time.Time) error {
	if date := now.Format(time.DateOnly); date != r.day.Date {
		if err := r.Flush(); err != nil {
			return err
		}
		d, err := loadDay(r.dir, date)
		if err != nil {
			return err
		}
		r.day = d
	}
	open := make(map[string][2]int64, len(snap.Connections))
	for i := range snap.Connections {
		c := &snap.Connections[i]
		open[c.ID] = [2]int64{c.Upload, c.Download}
		p, known := r.prev.Open[c.ID]
		var dUp, dDown int64
		switch {
		case known:
			dUp, dDown = c.Upload-p[0], c.Download-p[1]
		case r.first && !c.Start.After(r.prev.SavedAt):
			// open before we last looked (or before we ever ran): its earlier
			// bytes are unaccounted for, so start counting from now
			continue
		default:
			dUp, dDown = c.Upload, c.Download
		}
		if c.STUN() {
			continue
		}
		dUp, dDown = max(0, dUp), max(0, dDown)
		for dim := range Dims {
			g := r.day.Group(dim)
			k := key(c, dim)
			e := g[k]
			if e == nil {
				e = &Entry{}
				g[k] = e
			}
			if !known {
				e.Conns++
			}
			e.Up += dUp
			e.Down += dDown
		}
		if dUp+dDown > 0 || !known {
			r.dirty = true
		}
	}
	r.prev = state{SavedAt: now, Open: open}
	r.first = false
	return nil
}

// Flush writes today's totals and the open-connection state.
func (r *Recorder) Flush() error {
	if r.first {
		return nil // nothing observed yet; keep the old state for the next run
	}
	if r.dirty {
		if err := writeJSON(dayPath(r.dir, r.day.Date), r.day); err != nil {
			return err
		}
		r.dirty = false
	}
	return writeJSON(filepath.Join(r.dir, "state.json"), r.prev)
}
