# 8788 独立常驻服务

使用命名 LaunchAgent 可以同时保留原 `18828` 前向观察服务和 `8788` 影子账户服务。留空 `--name` 仍管理原默认服务，指定名称后只操作该名称对应的plist和日志。

本机影子服务程序位于 `~/.local/share/astock-shadow-service/bin/astock`。安装前先使用 `--dry-run` 核对程序、端口、数据目录及代理；它不会停止或安装服务。更换临时进程时，应核对当前监听PID和参数，避免两个实例争用同一端口。

```sh
~/.local/share/astock-shadow-service/bin/astock service install \
  --name shadow-8788 --listen 127.0.0.1:8788 \
  --data-home ~/.local/share --config-home ~/.config --cache-home ~/.cache \
  --http-proxy http://127.0.0.1:7897 --https-proxy http://127.0.0.1:7897 \
  --no-proxy 127.0.0.1,localhost --mac-notifications

~/.local/share/astock-shadow-service/bin/astock service status --name shadow-8788
```

代理地址使用本机现有配置，其他机器须按实际地址设置。安装时显式写入的 `HTTP_PROXY`、`HTTPS_PROXY` 和 `NO_PROXY` 会随进程自动恢复，不依赖交互式终端环境。代理进程本身仍需可用。

- 服务标识：`com.github.wenzhe.astock-workbench.web.shadow-8788`。
- 配置：`~/Library/LaunchAgents/com.github.wenzhe.astock-workbench.web.shadow-8788.plist`。
- 日志：`~/Library/Logs/astock-workbench/web-shadow-8788.out.log` 和 `web-shadow-8788.err.log`。
- 行为：用户登录后启动，进程退出后自动拉起，启动限流间隔30秒。
- 保留：原 `com.github.wenzhe.astock-workbench.web` 服务及其18828端口、数据目录不变。

这是用户级服务，Mac仍需保持登录、唤醒并可联网。Mac通知沿用当前开启状态；只要这个实例启用通知，就不要再对同一数据目录另启一个通知实例。

卸载命名服务使用 `service uninstall --name shadow-8788`。这只移除该服务配置，不删除账户或行情数据。更新程序后重新安装同名服务可加载新配置；若旧任务退出与重新加载之间短暂失败，应先核对 `service status --name shadow-8788`，避免额外启动重复实例。
