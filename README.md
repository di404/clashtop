# ClashTop

在终端里监控 Clash / mihomo：流量、连接、日志和流量统计。

## 安装

从 [Releases](https://github.com/di404/clashtop/releases) 下载，或：

```bash
go install github.com/di404/clashtop@latest
```

## 使用

```bash
clashtop                                   # 自动连接 Clash Verge
clashtop -url 127.0.0.1:9090 -secret xxx   # 指定 external-controller
```

首次运行会在后台安装一个服务，记录每天的流量统计（macOS）。卸载：

```bash
clashtop daemon uninstall
```

## License

MIT
