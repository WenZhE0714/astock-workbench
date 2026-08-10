# Architecture

## 目标

首版同时满足两个目标：实时行情必须轻量、稳定、可原地刷新；多智能体研究可以较重、较慢，
但不能拖垮行情界面。为此采用 Go 主程序与 Python 研究引擎的进程隔离，而不是把所有依赖
打进一个巨大二进制。

```text
腾讯 Level-1 ─────────> market adapter ──> Quote ─────> terminal UI
东方财富主力资金流 ───> market adapter ──> FundFlow ──┘
东方财富 F10/板块资金 ─> market adapter ──> BoardFlow ─┘
东方财富龙虎榜 ────────> market adapter ──> DragonTigerSnapshot ─┘
                                             │
                                             └──> future strategy input

TradingAgents-Astock ──> Python bridge ──> AnalysisResult(JSON)
                                             │
                         ┌───────────────────┼──────────────────┐
                         v                   v                  v
                    report store       strategy layer      audit trail
                                             │
                                             v
                                      deterministic RiskGate
                                      /                    \
                              PaperBroker              LiveBroker
```

## 模块边界

- `internal/market`：个股/指数行情、个股与板块主力资金流、关联板块排行、龙虎榜和名称解析适配器，不包含策略逻辑。
- `internal/ui`：纯终端渲染，输入是标准 `Quote`、`FundFlow`、`BoardFlow` 与 `DragonTigerSnapshot`；热门标签只基于已采集的同类排名和板块广度。
- `internal/analysis`：内嵌 Python bridge，以子进程调用 TradingAgents。
- `internal/domain`：跨模块稳定对象，尤其是 `AnalysisResult`。
- `internal/storage`：自选、缓存和报告归档；采用原子写入。
- 自选文件在原有逐行代码格式上增加 `[分组名]` 标题；无标题旧数据归入“默认”，加载“全部”时按分组顺序去重汇总，组内顺序独立持久化。
- `internal/strategy`：研究信号接口，不允许直接提交订单。
- `internal/backtest`：历史仿真接口，与执行系统隔离。
- `internal/execution`：订单、Broker 和确定性 RiskGate 契约。
- `internal/paper`：未来模拟账户与 A 股撮合规则。

## 为什么不把 TradingAgents 打进 Go 二进制

TradingAgents 依赖 Python、LangGraph、数据工具和多家模型 SDK。强行嵌入会显著增加体积，
还会让行情工具受 Python 包冲突和模型故障影响。子进程边界使实时看盘始终可以独立运行，
同时允许研究引擎单独升级或切换 provider。

## 分析契约

Python bridge 只输出 schema version 1 JSON，不把 LangGraph 内部对象泄漏给 Go。字段包括：

- 分析标的、交易日期、创建时间和运行耗时；
- `Buy / Overweight / Hold / Underweight / Sell` 五档组合评级；
- provider、深度模型、快速模型和引擎版本；
- 市场、情绪、新闻、基本面、政策、资金、解禁等报告；
- 多空研究、风险辩论、交易员与组合经理结论；
- 数据供应商和非投资建议声明。

后续 schema 变更必须增加版本并保持读取旧报告的迁移策略。

## 后续模拟盘与量化交易的门禁

实盘能力不得直接消费 LLM 文本。推荐事件链：

1. 策略将行情、历史数据和研究报告转成结构化 `Signal`；
2. 信号必须包含时间尺度、触发条件、失效条件、置信度和最大仓位；
3. RiskGate 校验资金、集中度、T+1、整手、涨跌停、停牌和当日损失上限；
4. 模拟盘先运行足够长的只读/影子期；
5. 实盘 Broker 默认需要人工确认，并保留不可篡改的信号—风控—委托审计链。

## 回测质量约束

- 行情、财务和股票池都必须是 point-in-time 数据；
- 不得在同一策略中混用前复权、后复权和不复权价格；
- 处理上市、退市、停牌和涨跌停导致的不可成交；
- 纳入佣金、印花税、过户费、滑点与冲击成本；
- 训练、验证、走样本外和实盘影子期分离；
- 报告绝对收益、相对基准、最大回撤、换手和容量，而不只展示胜率。
