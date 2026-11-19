# ClashTop

在终端里监控 Clash / mihomo：流量、连接、日志和流量统计。

## 安装

```bash
go install github.com/di404/clashtop@latest
```

## 使用

```bash
clashtop                                   # 自动连接 Clash Verge
clashtop -url 127.0.0.1:9090 -secret xxx   # 指定 external-controller
```

数字键 `1`–`4` 切换页面，`?` 查看快捷键。

## 历史统计

首次运行会自动在后台安装一个记录服务，用来保存每天的流量统计（仅 macOS）。

```bash
clashtop daemon status       # 查看状态
clashtop daemon uninstall    # 卸载
```

## License

MIT
