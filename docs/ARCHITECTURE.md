# Architecture

## 目标

首版同时满足两个目标：实时行情必须轻量、稳定、可原地刷新；多智能体研究可以较重、较慢，
但不能拖垮行情界面。为此采用 Go 主程序与 Python 研究引擎的进程隔离，而不是把所有依赖
打进一个巨大二进制。

```text
腾讯 Level-1 ─────────> market adapter ──> Quote ─────> terminal UI
东方财富主力资金流 ───> market adapter ──> FundFlow ──┘
                                       └──> 6分钟内存采样 ─> FundMovement ─> terminal UI
东方财富全行业资金 ─────> market adapter ──> BoardFlow ────────┘
东方财富 F10/板块资金 ─> market adapter ──> BoardFlow ─┘
东方财富龙虎榜 ────────> market adapter ──> DragonTigerSnapshot ─┘
东方财富/腾讯未复权日K ─> market adapter ──> DailyBar ─> technical strategy
新浪沪深京5分钟成交额 ───> market adapter ──> MarketAmountSnapshot ─┘
                                             │
                                             └──> future strategy input

全市场/行业横截面 + 未复权日K + 公告索引 ─> MarketScanFacts
                                                │
                         ┌──────────────────────┴──────────────────────┐
                         v                                             v
              deterministic report                     read-only Codex synthesis
                         └──────────────────────┬──────────────────────┘
                                                v
                                    market report archive + terminal UI

个股行情 + 资金/板块/龙虎榜 + 未复权日K + 公告/新闻线索
                         └──> StockReportFacts ──> read-only Codex/fallback
                                                       │
                                                       v
                                             per-stock report archive

当前股票 StockReportFacts + 本轮内存对话 ─> ephemeral read-only Codex
                                             │
                                             v
                                  background AI answer + terminal UI

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

- `internal/market`：个股/指数行情、沪深个股涨跌幅/涨速榜、未复权日 K、沪深京历史成交额、个股与全行业主力资金流、行业板块成份股成交额排行、关联板块排行、龙虎榜和名称解析适配器，不包含策略逻辑。个股榜单直接携带东方财富行业分类；日 K 先使用东方财富，自动回退腾讯未复权序列，再回退14天内本地有效缓存；上一交易日成交额使用新浪上证指数、深证综指和北证 50 的 5 分钟精确成交额按日汇总。
- `internal/app`：持有交互状态与异步轮询。9:15 集合竞价开始行情轮询，连续交易与可轮询时段分开建模，避免竞价数据进入盘中严格筛选。看盘启动后先在后台预热当前自选池；资金雷达按 10 秒记录累计主力净额，保留 6 分钟内存样本并派生 1/3/5 分钟 `FundMovement`，默认按 1 分钟净流入降序展示，行业快照按 60 秒刷新。采样状态与雷达可见状态分离，按 `v` 只显示并立即复用已有历史；分组或自选变化会重新绑定采样池。`y` 板块资金看板以独立 goroutine 获取双向 Top 5，并用最多 4 个 worker 查询 10 个板块的成交额前三成份股；看板可见时自动刷新间隔不低于 60 秒，局部成份股失败保留其他板块，整轮失败保留旧快照。智能市场扫描和 `x` 个股 AI 问答均在独立 goroutine 中采集、调用综合器并通过有界 channel 回到看盘事件循环，不阻塞行情刷新；问答历史仅驻留当前进程内存。
- `internal/ui`：纯终端渲染，输入是标准 `Quote`、`DailyBar` 派生信号、`FundFlow`、`FundMovement`、`BoardFlow`、`DragonTigerSnapshot` 或已生成的报告；热门和资金行为标签只基于已采集数据，不产生确定性交易指令。
- `internal/analysis`：内嵌 Python bridge，以子进程调用 TradingAgents；市场、个股报告和看盘问答以临时会话、只读沙箱和非交互模式调用 Codex，只接收已采集的结构化 JSON 与当前内存对话，不读取仓库、不搜索网络、不调用交易接口。
- `internal/domain`：跨模块稳定对象，包括带交易日和沪深京分项的 `MarketAmountSnapshot`、`MarketScanFacts`、`StockReportFacts` 以及 `AnalysisResult`。
- `internal/storage`：自选、最近查看历史、缓存和报告归档；采用原子写入。智能市场报告按时间戳保存，个股研判再按股票代码分目录保存 Markdown、结构化快照和元数据。
- 自选文件在原有逐行代码格式上增加 `[分组名]` 标题；无标题旧数据归入“默认”，加载“全部”时按分组顺序去重汇总，组内顺序独立持久化。
- 最近查看历史使用独立的有界 MRU 文件，只记录通过交互式查看命令成功打开的股票。
- `internal/strategy`：研究信号接口和确定性日线量价策略，不允许直接提交订单。技术信号必须包含方向、条件触发、失效位和仓位计划。
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
