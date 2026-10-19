# Mac 原生通知

在运行 Web 服务的 Mac 上，通过 macOS 通知中心接收候选触发和影子账户买卖消息。浏览器可以关闭，Mac 需保持登录、唤醒且 Web 服务运行。它是本地原生通知，不是手机或远程设备推送。

```sh
./dist/astock web --test-mac-notification
./dist/astock web --listen 127.0.0.1:8788 --mac-notifications
```

优先使用 [terminal-notifier 3.1.0](https://github.com/julienXX/terminal-notifier/releases/tag/3.1.0) 发送可点击通知，点击后在原服务端口打开对应事件。可通过 Homebrew 安装，或将官方预编译应用放在 `~/Applications/terminal-notifier.app`；需要在系统弹窗中允许通知。没有该工具时继续使用 macOS 自带的 AppleScript 通知，但兼容通道不支持点击定位。

测试消息提交成功但未见横幅时，在“系统设置 → 通知 → terminal-notifier”（兼容通道为“脚本编辑器”）允许通知并开启横幅/声音，同时检查专注模式。服务只能确认系统调用是否成功，不能确认用户是否看到了横幅。

- 候选：连续交易时段，新扫描中状态为 `triggered` 且 `portfolio_eligible=true`，报价和扫描时间在5分钟内。相同股票每个交易日最多提醒一次；普通观察状态不会提醒。候选提醒不是买入指令。
- 买卖：账户保存成功后，新增 `realtime-*` 来源的模拟成交。包括开仓、加仓、减仓、退出、风险退出和做T；标题注明模拟，副标题注明账户。拒单、历史日线回放、显式重建与旧成交不会弹出。
- 记录：`$XDG_DATA_HOME/astock-workbench/notifications/mac.json`，默认 `~/.local/share/astock-workbench/notifications/mac.json`。记录按事件去重，发送失败最多尝试3次，超过5分钟不再补弹。`submitted` 仅表示已提交给 macOS。重启保留去重记录，未完成且仍新鲜的发送可继续。
- 关闭：启动 Web 时省略 `--mac-notifications`。浏览器“桌面提醒”开关控制的是另一条计划监控通道，不影响 Mac 原生通知。

“监控中心 → Mac 通知”可以查看原消息并标为已读/已处理、添加备注、重点跟踪股票，或将股票静音到上海时区次日零点。处理记录保存在独立的 `notifications/reviews.json`，不会改写发送证据或模拟成交。新记录包含股票与账户标识；旧记录缺少这些字段时保留原文与处理功能，不猜测股票归属。

可选的登录自动启动服务也支持此参数：

```sh
./dist/astock service install --name shadow-8788 --listen 127.0.0.1:8788 --mac-notifications
```

安装服务会替换同名 ASTOCK LaunchAgent。使用独立的 `--name` 可与其他端口的服务共存，固定代理及数据目录的示例见 [8788 独立常驻服务](shadow-service.md)。共享同一数据目录的多个实例应只在其中一个实例启用通知。
