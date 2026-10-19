# 前瞻首日自动验收

`cmd/forward-acceptance`是独立的只读检查程序。它复用已有存储校验，对配置、源研究、日档哈希链和采集程序指纹进行核验，再检查最早一份日档的四组账户。它不执行采集程序，不访问行情网络，不改变冻结策略，不产生交易，也不把历史演练纳入观察样本。

## 检查内容

- 首日日档无前序指纹，四组账户完整，股票池及基准均有首日日K和足够的预热数据。
- 采集时间在当日15:30至19:00，日期不早于冻结起点且不晚于验收时间。
- 四组请求与冻结配置一致，双倍成本的费用和滑点正确。
- 首日无成交、无持仓、无费用，现金和净值等于各自初始资金，无回撤。
- 待执行订单只允许来自首日的买入信号，不允许重复股票。
- 候选信号精确等于原信号中量比至少1.5的部分，其余信号须有对应过滤记录；双倍成本组首日信号一致。
- 首日不得满足样本数量或允许晋级。没有信号也可以通过初始状态验收。

## 状态与产物

输出目录必须位于前瞻归档之外；每次原子更新`status.json`和`report.md`。配置指纹通过命令行显式固定，配置变化不会被自动接纳为同一个验收对象。

| 状态 | 含义 |
|---|---|
| waiting | 完整性检查通过，尚无首日日档 |
| attention | 起始日期的采集窗口已过，仍无首日日档，需要核对交易日安排和采集日志 |
| error | 配置、源研究、程序或日档校验失败，或采集任务报告错误 |
| passed | 最早日档符合首日验收条件 |

`attention`不直接认定漏采交易日，因为本程序没有独立交易所日历；假日也需要结合采集状态核对。`passed`只说明首日初始状态合格，不表示后续成交或策略效果通过。后续运行仍检查整个归档的完整性，但不会代替逐笔成交复核。程序只更新本地报告及日志，不发送通知。

## 运行

```sh
go build -trimpath -o /path/to/astock-forward-acceptance ./cmd/forward-acceptance
/path/to/astock-forward-acceptance \
  --archive /path/to/frozen-forward \
  --binary /path/to/frozen-collector \
  --output /path/to/separate-acceptance-output \
  --fingerprint EXPECTED_PLAN_FINGERPRINT
```

新输出目录的父目录需要已存在。使用配置中保存的程序SHA-256核验采集程序，验收程序本身不替换该程序。

## 本机任务

- LaunchAgent：`com.github.wenzhe.astock-workbench.volume-forward-acceptance`，登录时运行并每300秒检查。
- 程序：`~/.local/share/astock-volume-forward-service/bin/astock-forward-acceptance-v1`。
- 输入：`~/astock-volume-forward-2026-10-19-v2`。
- 输出：`~/astock-volume-forward-acceptance-2026-10-19`。
- 日志：`~/Library/Logs/astock-workbench/volume-forward-acceptance.out.log`和同名`.err.log`。
- 观察起点：2026-10-20；10月19日的正常状态是`waiting`、0个观察日。

该任务与采集任务分别调度，日档生成后通常在下一次检查时反映验收结果。两者均依赖Mac登录且处于唤醒状态；本任务不更改系统休眠设置。

## 验证

`go test ./cmd/forward-acceptance`覆盖无信号、量比边界、提前成交、资金和成本漂移、未来信号、缺K、过滤记录、等待和窗口结束、日档通过及指纹变化。`go vet ./cmd/forward-acceptance`通过。

设置`ASTOCK_ACCEPTANCE_REHEARSAL_ARCHIVE`后，额外只读加载既有演练档案并验证其中首日日档。本机已验证历史演练的2026-07-16日档；这一测试不会生成正式验收通过状态或迁移任何日档。
