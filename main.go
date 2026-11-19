package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/di404/clashtop/internal/api"
	"github.com/di404/clashtop/internal/ui"
	"github.com/di404/clashtop/internal/usage"
)

// Sockets used by common Clash front-ends (Clash Verge Rev and friends).
var knownSockets = []string{
	"/tmp/verge/verge-mihomo.sock",
	"/tmp/verge-mihomo.sock",
	"/var/tmp/verge/verge-mihomo.sock",
}

type endpoint struct {
	addr, socket, secret string
}

func endpointFlags(fs *flag.FlagSet) *endpoint {
	e := &endpoint{}
	fs.StringVar(&e.addr, "url", os.Getenv("CLASH_API"), "external-controller 地址，如 127.0.0.1:9090（环境变量 CLASH_API）")
	fs.StringVar(&e.socket, "socket", os.Getenv("CLASH_SOCKET"), "external-controller-unix 套接字路径（环境变量 CLASH_SOCKET）")
	fs.StringVar(&e.secret, "secret", os.Getenv("CLASH_SECRET"), "API 密钥（环境变量 CLASH_SECRET）")
	return e
}

// args are the flags that reproduce this endpoint for the daemon. Nothing
// explicit means the daemon auto-detects on every reconnect.
func (e *endpoint) args() []string {
	var a []string
	if e.addr != "" {
		a = append(a, "-url", e.addr)
	} else if e.socket != "" {
		a = append(a, "-socket", e.socket)
	}
	if e.secret != "" {
		a = append(a, "-secret", e.secret)
	}
	return a
}

func (e *endpoint) client() *api.Client {
	addr, socket := e.addr, e.socket
	if addr == "" && socket == "" {
		for _, s := range knownSockets {
			if fi, err := os.Stat(s); err == nil && fi.Mode()&os.ModeSocket != 0 {
				socket = s
				break
			}
		}
		if socket == "" {
			addr = "127.0.0.1:9090"
		}
	}
	if addr != "" {
		socket = "" // an explicit URL wins over an env socket
	}
	return api.New(addr, socket, e.secret)
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "daemon" {
		daemon(os.Args[2:])
		return
	}
	fs := flag.NewFlagSet("clashtop", flag.ExitOnError)
	ep := endpointFlags(fs)
	interval := fs.Duration("interval", time.Second, "连接列表刷新间隔")
	level := fs.String("log-level", "info", "日志订阅级别：debug / info / warning / error")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "用法: clashtop [参数]\n      clashtop daemon [install|uninstall|status] [参数]")
		fs.PrintDefaults()
	}
	_ = fs.Parse(os.Args[1:])

	dir, err := usage.Dir()
	if err != nil {
		fail(err)
	}
	opt := ui.Options{Interval: *interval, LogLevel: *level, UsageDir: dir, Notice: ensureDaemon(dir, ep)}
	if err := ui.Run(ep.client(), opt); err != nil {
		fail(err)
	}
}

// ensureDaemon registers the background recorder the first time the TUI runs,
// and repoints it if the binary it ran was moved or deleted. It returns a
// notice for the UI, or "".
func ensureDaemon(dir string, ep *endpoint) string {
	if runtime.GOOS != "darwin" || usage.OptedOut(dir) {
		return ""
	}
	exe, err := executable()
	if err != nil || strings.Contains(exe, "go-build") { // `go run` temp binary
		return ""
	}
	installed, loaded, cur := usage.Status()
	if installed && loaded {
		if _, err := os.Stat(cur); err == nil {
			return ""
		}
	}
	if installed && !loaded && cur != "" {
		if _, err := os.Stat(cur); err == nil {
			exe = cur // just reload it, don't steal it for this binary
		}
	}
	if err := usage.Install(dir, exe, append([]string{"daemon"}, ep.args()...)); err != nil {
		return "后台记录注册失败: " + err.Error()
	}
	if installed {
		return ""
	}
	return "已注册开机启动的后台记录（clashtop daemon uninstall 可移除）"
}

func executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

func daemon(args []string) {
	action := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		action, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("clashtop daemon", flag.ExitOnError)
	ep := endpointFlags(fs)
	interval := fs.Duration("interval", time.Second, "采样间隔")
	_ = fs.Parse(args)
	dir, err := usage.Dir()
	if err != nil {
		fail(err)
	}

	switch action {
	case "":
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		log.SetFlags(log.LstdFlags)
		log.Printf("recording to %s", dir)
		if err := usage.Run(ctx, dir, ep.client, *interval); err != nil {
			fail(err)
		}
	case "install":
		exe, err := executable()
		if err != nil {
			fail(err)
		}
		if err := usage.Install(dir, exe, append([]string{"daemon"}, ep.args()...)); err != nil {
			fail(err)
		}
		fmt.Println("已安装并启动:", usage.Label)
	case "uninstall":
		if err := usage.Uninstall(dir); err != nil {
			fail(err)
		}
		fmt.Println("已移除:", usage.Label, "（数据保留在", filepath.Join(dir, "usage")+"）")
	case "status":
		installed, loaded, exe := usage.Status()
		switch {
		case !installed:
			fmt.Println("未安装")
		case loaded:
			fmt.Println("运行中:", exe)
		default:
			fmt.Println("已安装但未加载:", exe)
		}
		fmt.Println("数据目录:", filepath.Join(dir, "usage"))
	default:
		fail(fmt.Errorf("未知子命令 %q（可用: install / uninstall / status）", action))
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
