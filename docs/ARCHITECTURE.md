# Architecture

## 目标

首版同时满足两个目标：实时行情必须轻量、稳定、可原地刷新；多智能体研究可以较重、较慢，
但不能拖垮行情界面。为此采用 Go 主程序与 Python 研究引擎的进程隔离，而不是把所有依赖
打进一个巨大二进制。

本地 Web 使用独立 Vue 前端和 Go `net/http` 只读 API，生产构建产物通过 `embed` 进入同一个
`astock` 二进制。CLI 和 Web 复用同一组行情适配器、日 K 缓存和后续实验存储，不在浏览器侧实现
第二套行情或回测逻辑。首版 `/api/stock` 并发获取腾讯 Level-1 快照与东方财富/腾讯未复权日 K，
Web 只负责日 K、成交量、均线和涨跌停边界的交互呈现。

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

全市场/行业横截面 + 未复权日K + 公告/新闻/券商观点 ─> MarketScanFacts
                                                │
                         ┌──────────────────────┴──────────────────────┐
                         v                                             v
              deterministic report                     read-only Codex synthesis
                         └──────────────────────┬──────────────────────┘
                                                v
                                    market report archive + terminal UI

个股行情 + 资金/板块/龙虎榜 + 未复权日K + 公告/新闻/券商观点
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

- `internal/market`：个股/指数行情、沪深个股涨跌幅/涨速榜、未复权日 K、沪深京历史成交额、个股与全行业主力资金流、行业板块成份股成交额排行、关联板块排行、龙虎榜、公告索引、新闻搜索、券商研报索引和名称解析适配器，不包含策略逻辑。个股榜单直接携带东方财富行业分类；日 K 先使用东方财富，自动回退腾讯未复权序列，再回退14天内本地有效缓存；上一交易日成交额使用新浪上证指数、深证综指和北证 50 的 5 分钟精确成交额按日汇总。
- `internal/app`：持有交互状态与异步轮询。9:15 集合竞价开始行情轮询，连续交易与可轮询时段分开建模，避免竞价数据进入盘中严格筛选。看盘启动后先在后台预热当前自选池；资金雷达按 10 秒记录累计主力净额，保留 6 分钟内存样本并派生 1/3/5 分钟 `FundMovement`，默认按 1 分钟净流入降序展示，行业快照按 60 秒刷新。采样状态与雷达可见状态分离，按 `v` 只显示并立即复用已有历史；分组或自选变化会重新绑定采样池。`y` 板块资金看板以独立 goroutine 获取双向 Top 5，并用最多 4 个 worker 查询 10 个板块的成交额前三成份股；看板可见时自动刷新间隔不低于 60 秒，局部成份股失败保留其他板块，整轮失败保留旧快照。智能市场扫描、个股研判、`x` 个股 AI 问答和 `t` 策略研究任务均在独立 goroutine 中执行并通过有界 channel 回到看盘事件循环，不阻塞行情刷新；不同任务可并行，同类任务只允许一个实例，所有任务状态按行聚合。策略页冻结进入时的股票或列表上下文，后台 goroutine 只读取任务快照。
- `internal/ui`：纯终端渲染，输入是标准 `Quote`、`DailyBar` 派生信号、`FundFlow`、`FundMovement`、`BoardFlow`、`DragonTigerSnapshot`、回测/优化摘要或已生成的报告；策略研究中心使用同一滚动渲染器展示菜单、设置、历史、候选与交易证据，不持有市场或撮合逻辑。
- `internal/analysis`：内嵌 Python bridge，以子进程调用 TradingAgents；市场、个股报告和看盘问答以临时会话、只读沙箱和非交互模式调用 Codex，只接收已采集的结构化 JSON 与当前股票最近6轮问答，不读取仓库、不搜索网络、不调用交易接口。外部信息由 Go 采集一次并冻结，所有子 Agent 使用同一快照；模型不能自行扩充来源。
- `internal/domain`：跨模块稳定对象，包括带交易日和沪深京分项的 `MarketAmountSnapshot`、`EvidenceSnapshot`、`MarketScanFacts`、`StockReportFacts` 以及 `AnalysisResult`。凭证按 A 已核验官方披露正文、B 专业观点、C 公告/新闻待核线索、D 市场情绪分层；正文未核验的公告索引使用 `disclosure_index` 且保留 `verified_body=false`。
- `internal/storage`：自选、最近查看历史、AI咨询历史、缓存和报告归档；采用原子写入。AI问答按股票保存最近100轮，模型上下文只读取最近6轮。智能市场报告按时间戳保存，个股研判再按股票代码分目录保存 Markdown、结构化快照、Agent 结果、独立 `evidence.json` 和元数据；索引按元数据生成时间倒序读取，交互层按日期和日期内时间分组展示，无需迁移旧归档。单次回测与优化实验分目录存储；优化实验包含候选表、入选参数及训练/验证/样本外的独立交易和资金曲线。
- 自选文件在原有逐行代码格式上增加 `[分组名]` 标题；无标题旧数据归入“默认”，加载“全部”时按分组顺序去重汇总，组内顺序独立持久化。
- 最近查看历史使用独立的有界 MRU 文件，只记录通过交互式查看命令成功打开的股票。
- `internal/strategy`：研究信号接口和确定性日线量价策略，不允许直接提交订单。技术信号必须包含方向、条件触发、失效位和仓位计划。
- `internal/backtest`：独立的日线历史仿真引擎，与执行系统隔离。信号只读取当日收盘及更早数据，成交安排在下一交易日开盘；结果包含交易审计、每日权益、回撤和成本。回测固定不复权口径并显式标记静态股票池、公司行动和日K成交概率限制。优化器用有界确定性候选完成训练/验证排序，锁定参数后才单次调用样本外区间；样本外不能进入评分或重选。持续优化器进一步使用多折滚动训练/验证、完全非重叠的最终留出、双倍成本和最佳单笔利润集中度压力测试；实验清单固定记录候选集与配置指纹，数据质量、Agent共识和参数邻域共同决定是否只能停留在研究阶段。
- 多Agent策略研究由 Go 主协调器掌握协议、候选去重、评分和晋级；Codex 子Agent只在独立空临时目录中输出受 JSON Schema 约束的参数与假设，不能读取最终留出、修改账户/费用/门禁或触发交易。主Agent复盘发生在所有确定性结果锁定之后，只解释不可变结果。
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
- AI 优化层只能提出结构化参数候选，回测引擎和历史归档不可由模型改写；训练、验证和样本外结果必须分别保留。

策略研究生命周期和开源项目借鉴见 [STRATEGY_RESEARCH.md](STRATEGY_RESEARCH.md)。
