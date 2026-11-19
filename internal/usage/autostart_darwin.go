package usage

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const Label = "com.clashtop.daemon"

func plistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", Label+".plist"), nil
}

func domain() string { return fmt.Sprintf("gui/%d", os.Getuid()) }

// Supported reports whether this system can run the recorder in the background.
func Supported() bool { return true }

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// Install writes a LaunchAgent that runs `exe args...` at login and keeps it
// alive, then (re)loads it.
func Install(dir, exe string, args []string) error {
	p, err := plistPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var prog strings.Builder
	for _, a := range append([]string{exe}, args...) {
		prog.WriteString("\t\t<string>" + xmlEscape(a) + "</string>\n")
	}
	logPath := xmlEscape(filepath.Join(dir, "daemon.log"))
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + Label + `</string>
	<key>ProgramArguments</key>
	<array>
` + prog.String() + `	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>ProcessType</key>
	<string>Background</string>
	<key>StandardErrorPath</key>
	<string>` + logPath + `</string>
	<key>StandardOutPath</key>
	<string>` + logPath + `</string>
</dict>
</plist>
`
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	// 0600: the arguments may carry the API secret
	if err := os.WriteFile(p, []byte(plist), 0o600); err != nil {
		return err
	}
	_ = exec.Command("launchctl", "bootout", domain()+"/"+Label).Run()
	if out, err := exec.Command("launchctl", "bootstrap", domain(), p).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl bootstrap: %v %s", err, bytes.TrimSpace(out))
	}
	_ = os.Remove(optOutPath(dir))
	return nil
}

// Uninstall unloads and removes the LaunchAgent and remembers the opt-out.
func Uninstall(dir string) error {
	p, err := plistPath()
	if err != nil {
		return err
	}
	_ = exec.Command("launchctl", "bootout", domain()+"/"+Label).Run()
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(optOutPath(dir), nil, 0o644)
}

// Status reports whether the agent is installed, loaded, and which program it runs.
func Status() (installed, loaded bool, exe string) {
	p, err := plistPath()
	if err != nil {
		return
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	installed = true
	if i := bytes.Index(b, []byte("<array>")); i >= 0 {
		rest := b[i:]
		if s := bytes.Index(rest, []byte("<string>")); s >= 0 {
			if e := bytes.Index(rest[s:], []byte("</string>")); e >= 0 {
				exe = string(rest[s+len("<string>") : s+e])
				exe = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&#34;", `"`, "&#39;", "'").Replace(exe)
			}
		}
	}
	loaded = exec.Command("launchctl", "print", domain()+"/"+Label).Run() == nil
	return
}
