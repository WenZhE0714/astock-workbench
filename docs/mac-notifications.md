# Mac 原生通知

在运行 Web 服务的 Mac 上，通过 macOS 通知中心接收候选触发和影子账户买卖消息。浏览器可以关闭，Mac 需保持登录、唤醒且 Web 服务运行。它是本地原生通知，不是手机或远程设备推送。

```sh
./dist/astock web --test-mac-notification
./dist/astock web --listen 127.0.0.1:8788 --mac-notifications
```

测试消息提交成功但未见横幅时，在“系统设置 → 通知 → 脚本编辑器”允许通知并开启横幅/声音，同时检查专注模式。此通道使用 macOS 自带的 AppleScript 通知，通知来源可能显示为“脚本编辑器”，标题为 ASTOCK。服务只能确认系统调用是否成功，不能确认用户是否看到了横幅。

- 候选：连续交易时段，新扫描中状态为 `triggered` 且 `portfolio_eligible=true`，报价和扫描时间在5分钟内。相同股票每个交易日最多提醒一次；普通观察状态不会提醒。候选提醒不是买入指令。
- 买卖：账户保存成功后，新增 `realtime-*` 来源的模拟成交。包括开仓、加仓、减仓、退出、风险退出和做T；标题注明模拟，副标题注明账户。拒单、历史日线回放、显式重建与旧成交不会弹出。
- 记录：`$XDG_DATA_HOME/astock-workbench/notifications/mac.json`，默认 `~/.local/share/astock-workbench/notifications/mac.json`。记录按事件去重，发送失败最多尝试3次，超过5分钟不再补弹。`submitted` 仅表示已提交给 macOS。重启保留去重记录，未完成且仍新鲜的发送可继续。
- 关闭：启动 Web 时省略 `--mac-notifications`。浏览器“桌面提醒”开关控制的是另一条计划监控通道，不影响 Mac 原生通知。

可选的登录自动启动服务也支持此参数：

```sh
./dist/astock service install --listen 127.0.0.1:8788 --mac-notifications
```

安装服务会替换同名 ASTOCK LaunchAgent。已有其他端口的自动服务时，先核对原配置；不要为测试覆盖正在运行的其他实例。共享同一数据目录的多个实例应只在其中一个实例启用通知。
