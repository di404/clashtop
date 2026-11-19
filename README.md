# ClashTop

终端里监控 Clash / mihomo：实时流量、连接、日志、按进程/主机/节点/规则的流量统计。

```bash
go install github.com/di404/clashtop@latest   # 或 git clone 后 go build -o clashtop .
./clashtop                                   # 自动找 Clash Verge 的 unix socket，找不到用 127.0.0.1:9090
./clashtop -url 127.0.0.1:9090 -secret xxx   # 指定 external-controller
./clashtop -socket /tmp/verge/verge-mihomo.sock
```

参数也可用环境变量 `CLASH_API` / `CLASH_SOCKET` / `CLASH_SECRET`。其他：`-interval 1s`（连接刷新间隔）、`-log-level info`。

| 页 | 内容 |
| --- | --- |
| 1 概览 | 速度、总量、连接数、内存；上下行历史图；当前最活跃的主机和进程 |
| 2 连接 | 可排序/过滤的连接表，←→ 切换活动/已关闭，enter 看详情，f 冻结，x/X 断开 |
| 3 日志 | 实时日志，←→ 切换订阅级别，/ 过滤，w 换行 |
| 4 统计 | 按进程/主机/节点/规则累计的流量，←→ 切换维度；t 切换 本次运行 / 今天 / 7 天 / 30 天 |

按 `?` 查看全部快捷键。

## 后台记录

mihomo 不保存历史，连接关闭后字节数就没了，所以长期统计靠一个常驻的 `clashtop daemon`：每秒拉一次 `/connections`，按天把各维度流量写到 `~/Library/Application Support/clashtop/usage/YYYY-MM-DD.json`（约 15 秒落盘一次）。TUI 只读这些文件，不会重复计数。

首次打开 TUI 时会自动注册成 launchd 用户代理（`~/Library/LaunchAgents/com.clashtop.daemon.plist`，登录时启动、退出自动拉起），启动 TUI 时用的 `-url/-socket/-secret` 会一并写进去；没指定就由 daemon 每次重连时自动探测 socket。二进制被移走或删掉后，下次打开 TUI 会自动改指向当前这个。

```bash
./clashtop daemon status      # 是否在运行、数据目录
./clashtop daemon install     # 手动安装 / 重新加载（重新编译后用它让 daemon 跑新版本）
./clashtop daemon uninstall   # 移除，之后打开 TUI 也不会再自动注册；数据保留
./clashtop daemon             # 前台运行，调试用
```

UDP STUN 连接（`stun*` 主机或 3478/19302 等端口）的计数在 mihomo 里常常虚高到上百 GB：这类连接仍列在连接页（淡色），但不计入速度、统计和走代理比例；它们存在期间，速度改用其余连接的增量求和，总量扣除本次运行观察到的 STUN 字节。

## 开发

`cmd/mockclash` 是一个假的 mihomo API，不需要真实 Clash 就能调界面：

```bash
go run ./cmd/mockclash -addr 127.0.0.1:19090 &
./clashtop -url 127.0.0.1:19090
```

## License

MIT
