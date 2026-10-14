# 前向任务运行与验收

前向任务在 Web 服务运行时自动采集。上海时间 15:30 至 18:00 是采集窗口，失败项每 10 分钟重试；进程退出、电脑睡眠或关机期间无法采集。用户级 launchd 服务在登录后启动，并在进程退出后重新拉起；它不会自动唤醒睡眠中的电脑。

## 运行监控

“形态验证 → 前向观察 → 运行监控”展示：

- 当日留存数量、历史缺采日和未衔接结果日志的采集日数量。
- 下次计划采集、最近完整采集、最近调度结束时间。
- 每日完成情况、缺采证券，以及原始快照入口。
- 日历、采集、跟踪和归档完整性异常；故障持续期间去重，恢复后追加恢复记录。
- 最近 300 条异常记录及确认状态。确认记录不改变任务、行情证据或缺采结论。

下次计划采集只使用已知交易日历。未知交易日不按工作日猜测；没有可确认的后续日期时显示 `--`。日历故障修复后会清除当前错误，并保留故障与恢复记录。

`GET /api/pattern-forwards` 返回任务 `health`、`operations` 及服务 `runtime`。`POST /api/pattern-forwards` 可用 `{action:"ack-alert",id,sequence}` 确认一条异常记录。`GET /api/pattern-forwards?id=...&audit=1` 只读校验原始配置、采集索引、快照及结果日志的引用与指纹，不重新取行情。

## 迁移临时任务

先构建并完成测试，停止源预览服务后执行正式迁移。`--from` 指向包含 `pattern-forwards` 和 `pattern-batches` 的 `astock-workbench` 数据目录。

```bash
export XDG_DATA_HOME="$HOME/.local/share/astock-forward-service/data"
export XDG_CONFIG_HOME="$HOME/.config/astock-forward-service"
export XDG_CACHE_HOME="$HOME/.cache/astock-forward-service"

./dist/astock forward import --from /path/to/preview/data/astock-workbench
./dist/astock forward import --from /path/to/preview/data/astock-workbench --apply
./dist/astock forward audit
```

默认只预检，不创建目标目录。`--apply` 复制前向任务、异常记录和任务引用的股票池版本，不迁移预览的影子账户、其他策略状态、缓存或凭据。源目录保留；现有同名文件内容不同时拒绝覆盖。任务目录整体发布，中断后可以重跑；尚未恢复的孤立快照会阻止迁移。预检输出每个任务的原始配置指纹，迁移后须再次核对。

## 常驻服务

安装时固定数据、配置和缓存根目录，不依赖 launchd 继承终端环境。已有其他实例时应选用独立的持久目录和空闲端口。

```bash
./dist/astock service install --listen 127.0.0.1:18828 --source http --symbol 002080 \
  --data-home "$XDG_DATA_HOME" --config-home "$XDG_CONFIG_HOME" \
  --cache-home "$XDG_CACHE_HOME" --dry-run

./dist/astock service install --listen 127.0.0.1:18828 --source http --symbol 002080 \
  --data-home "$XDG_DATA_HOME" --config-home "$XDG_CONFIG_HOME" \
  --cache-home "$XDG_CACHE_HOME"
./dist/astock service status
```

`--dry-run` 仅输出 plist。可用 `--calendar /absolute/path/calendar-file` 固定交易日历；省略时使用当前 `ASTOCK_TRADING_CALENDAR_FILE`，仍为空则使用沪深300日K参照。plist 只固定这些明确的环境变量，不复制终端凭据。

服务标签为 `com.github.wenzhe.astock-workbench.web`，配置位于 `~/Library/LaunchAgents/`，日志位于 `~/Library/Logs/astock-workbench/`。服务绑定安装命令自身的可执行文件路径；长期使用时应从固定安装位置运行 `service install`。更新程序后重新安装服务以加载新程序。`service uninstall` 停止服务并移除 plist，研究档案保留。

## 收盘验收

采集日封存后自动进行完整性验收，也可执行 `astock forward audit --id 任务ID` 或使用页面校验按钮。验收检查配置和股票池指纹、证券与样本索引、股票及基准快照、日志哈希链，以及每个日志对应的完整历史证据指纹。

- `waiting`：没有采集日，仅验证了配置，不能视为收盘验收通过。
- `collecting`：当前采集日尚未封存。
- `tracking-pending`：已封存快照尚未全部生成结果日志。
- `partial`：证据完整性通过，但包含明确的缺采日。
- `verified`：现有采集记录及结果日志通过完整性检查。
- `invalid`：存在损坏或引用不一致，须保留现场并排查。

对 20 只股票任务，首日应核对当日日K日期、留存数量、缺采原因、基准快照和同日结果日志；不足 20 只时如实记录缺采。验收不会补采过去的信号，指纹一致也不等于供应商数据或研究结论已经获得外部确认。

## 本机部署记录（2026-10-14）

本次已将临时预览中的真实任务迁入持久目录，以下路径相对当前用户的主目录：

- 可执行文件：`~/.local/share/astock-forward-service/bin/astock`。
- 数据根目录：`~/.local/share/astock-forward-service/data/astock-workbench`。
- 配置根目录：`~/.config/astock-forward-service`；缓存根目录：`~/.cache/astock-forward-service`。
- 服务地址：`http://127.0.0.1:18828/?view=strategy&strategy=patterns`。
- 任务：`682b926c71447a64bf9f33ab66ee5480`，跨行业20只前向观察。
- 冻结配置指纹：`6b07ab4a3b42b050282a7f4d856dd22978b98994f0ca0a252e3b8261e89feee7`。

迁移源 `/private/tmp/astock-forward-preview/data/astock-workbench` 已保留。迁移前后原始 `config.json` 逐字节一致。用户级 launchd 服务已安装，受控发送 SIGTERM 后自动启动新进程，任务配置和运行记录保持一致。13:51 的日历读取失败在 13:52 的后续调度中恢复，故障与恢复事件均已持久化。

本轮全量 Go 测试、`go vet ./...` 及前向存储、策略和 Web 定向竞态检查通过；前端测试与构建通过。模拟服务的日历故障恢复、异常确认、部分失败重试、归档校验和缺采流程通过 Playwright 验收；真实服务的 1440、390、320 像素宽页面检查通过，无页面脚本错误。

截至上海时间 13:59，真实任务已启用，归档状态为 `waiting`，采集数量为 `0/20`，下次计划采集为当日 15:30。此记录只证明迁移、服务恢复和采集前状态验收通过，首日真实快照及同日结果日志仍待收盘后核验。服务须保持运行，电脑须保持开机且未睡眠。
