package usage

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

const Label = "clashtop.service"

func unitPath() (string, error) {
	base, err := os.UserConfigDir() // $XDG_CONFIG_HOME or ~/.config
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "systemd", "user", Label), nil
}

func systemctl(args ...string) error {
	out, err := exec.Command("systemctl", append([]string{"--user"}, args...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl --user %s: %v %s", strings.Join(args, " "), err, bytes.TrimSpace(out))
	}
	return nil
}

// Supported reports whether a systemd user manager is reachable. Checked once
// per process: the UI asks on every render.
var Supported = sync.OnceValue(func() bool {
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return false
	}
	return exec.Command("systemctl", "--user", "show-environment").Run() == nil
})

// quoteArg quotes one ExecStart argument for systemd.
func quoteArg(s string) string {
	s = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%", "$", "$$").Replace(s)
	return `"` + s + `"`
}

// Install writes a systemd user unit that runs `exe args...` at login and
// restarts it on exit, then enables and (re)starts it.
func Install(dir, exe string, args []string) error {
	if !Supported() {
		return errors.New("需要 systemd 用户服务")
	}
	p, err := unitPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	q := make([]string, 0, len(args)+1)
	for _, a := range append([]string{exe}, args...) {
		q = append(q, quoteArg(a))
	}
	unit := `[Unit]
Description=ClashTop usage recorder

[Service]
ExecStart=` + strings.Join(q, " ") + `
Restart=always
RestartSec=5

[Install]
WantedBy=default.target
`
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	// 0600: the arguments may carry the API secret
	if err := os.WriteFile(p, []byte(unit), 0o600); err != nil {
		return err
	}
	if err := systemctl("daemon-reload"); err != nil {
		return err
	}
	if err := systemctl("enable", Label); err != nil {
		return err
	}
	if err := systemctl("restart", Label); err != nil {
		return err
	}
	_ = os.Remove(optOutPath(dir))
	return nil
}

// Uninstall stops and removes the unit and remembers the opt-out.
func Uninstall(dir string) error {
	p, err := unitPath()
	if err != nil {
		return err
	}
	_ = systemctl("disable", "--now", Label)
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	_ = systemctl("daemon-reload")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(optOutPath(dir), nil, 0o644)
}

// Status reports whether the unit is installed, running, and which program it runs.
func Status() (installed, loaded bool, exe string) {
	p, err := unitPath()
	if err != nil {
		return
	}
	f, err := os.Open(p)
	if err != nil {
		return
	}
	defer f.Close()
	installed = true
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if line, ok := strings.CutPrefix(sc.Text(), "ExecStart="); ok {
			exe = firstArg(line)
			break
		}
	}
	loaded = exec.Command("systemctl", "--user", "is-active", "--quiet", Label).Run() == nil
	return
}

// firstArg undoes quoteArg for the first argument of an ExecStart line.
func firstArg(line string) string {
	if !strings.HasPrefix(line, `"`) {
		exe, _, _ := strings.Cut(line, " ")
		return exe
	}
	var b strings.Builder
	for i := 1; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '\\' && i+1 < len(line):
			i++
			b.WriteByte(line[i])
		case (c == '%' || c == '$') && i+1 < len(line) && line[i+1] == c:
			i++
			b.WriteByte(c)
		case c == '"':
			return b.String()
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
