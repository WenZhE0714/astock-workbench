# astock-workbench

一个面向沪深 A 股的终端工作台：用小体积 Go 二进制实时看盘，用独立 Python 适配器调用
TradingAgents-Astock 做多智能体策略研究，并为模拟盘、回测和量化执行预留严格隔离的接口。

## 当前能力

| 能力 | 状态 | 说明 |
|---|---|---|
| 实时行情 | 可用 | 腾讯公开 Level-1，股票列表/完整详情原地切换，含资金流 |
| 股票名称/代码 | 可用 | 支持沪深代码、中文名称和歧义提示 |
| 自选股 | 可用 | 与现有 astock/astock-go 共用自选文件 |
| 摸鱼/拼音模式 | 可用 | 无颜色带框表格，含大盘与主力资金净流向 |
| 五档盘口 | 可用 | 标准视图下显示买卖五档 |
| 多智能体分析 | 可用 | 适配本地 TradingAgents-Astock，不复制其源码 |
| 分析归档 | 可用 | 每次保存完整 JSON 和 Markdown |
| 模拟盘 | 接口已预留 | 撮合、持仓流水尚未实现 |
| 回测 | 接口已预留 | 历史数据与 Backtrader 适配器尚未实现 |
| 实盘交易 | 仅接口边界 | 默认不存在实盘 Broker，研究信号不能直接下单 |

## 快速开始

要求 Go 1.24 或更高版本：

```bash
cd /Users/wenzhe/astock-workbench
make test
make build
./dist/astock watch --once 600519
```

持续看盘：

```bash
./dist/astock watch 贵州茅台 平安银行
./dist/astock watch --moyu 贵州茅台 平安银行
./dist/astock watch --pinyin 600519 000001
./dist/astock watch --depth 贵州茅台
./dist/astock watch --interval 5 贵州茅台   # 改为每 5 秒刷新
```

交易时段内默认每 1 秒刷新，可通过 `-i` 或 `--interval` 配置为 1–3600 秒。程序启动时
始终获取一次快照；午休、收盘后和周末保持界面可操作，但不再持续请求行情，进入下一个
交易时段后自动恢复。标题会显示市场状态和最近一次成功刷新的时间点。

持续模式使用终端备用屏幕原位重绘，不增加主终端滚动记录；按 `Ctrl-C` 退出后会恢复
运行前的 shell 画面。使用 `↑/↓` 或 `j/k` 选择个股，按 `Enter` 打开完整详情，按 `Esc`
返回列表；`PgUp/PgDn` 或 `b/空格` 用于跳选或详情翻页，`g/G` 选择首尾，`q` 退出。
在持续模式底部会显示当前可用操作；按 `a` 后输入代码或名称添加自选，按 `d` 删除箭头当前选中的自选并进行二次确认，
按 `i` 后输入代码或名称可直接查看该股票详情。名称匹配到多只股票时，可用 `↑/↓` 选择候选并按 `Enter` 确认。
命令输入支持退格，`Esc` 取消。
输入状态和快捷键操作栏分两行显示，输入位置带闪烁光标；操作完成或失败后的状态提示保留 5 秒后自动清除。
股票较多时列表会围绕当前选择自动取可见窗口。`--once` 仍以普通文本输出完整单次快照。

普通模式的列表用于快速选择，并显示个股主力资金净额、涨停和跌停；上涨/流入用 `↑`，下跌/流出用 `↓`，
标准模式保留红涨绿跌颜色。进入详情后包含当前价、涨跌、
昨收/今开、买一/卖一及挂单量、日内价格区间，以及成交量、成交额、换手、振幅、量比、均价、
PE(TTM)、PB、总/流通市值、涨跌停价和主力资金净额/净占比。`--depth` 会在详情中展开完整五档。
详情还会显示关联度最高的 6 个行业/概念板块，包括板块涨跌、主力净流入/净占比和领涨股，
并判断关联板块是多数流入、多数流出还是资金分化，以及个股资金方向是否与板块一致。
关联板块来自东方财富 F10 的行业与精确概念排名；地域、指数、风格和持仓标签不纳入这 6 项。
每个板块还会给出同类板块的涨幅/主力资金/换手 Top 100 排名、上涨/下跌家数，并标记热门、热门分歧、活跃、一般或偏冷。
若个股近 30 日登上龙虎榜，详情会列出最近 3 条上榜记录的触发原因、买卖额、净买入、榜单成交占比、换手、席位标签和可用的上榜后表现；这些是短线行为证据，不替代公告、财报或买卖决策。
`--moyu` 使用无色紧凑表格，同样显示个股资金流，并在列表顶部显示上证、深证、创业板的指数资金；
`↑` 表示净流入，`↓` 表示净流出，个股和板块资金流最多每 60 秒更新一次。顶部还显示沪深总成交额；
它由上证和深证成交额组成，不把创业板重复计入；若历史日线可用，还会显示相对上一交易日的
增减额和增幅；历史数据暂不可用时隐藏这项对比。历史值使用未复权日线成交额，避免把复权价格口径混入成交统计。
可配置 `ASTOCK_TUSHARE_TOKEN` 后，会自动使用 Tushare Pro `index_daily` 作为历史成交额回退。

板块排行和龙虎榜数据使用东方财富公开接口。可通过 `ASTOCK_BOARD_RANK_API_URL`、
`ASTOCK_DRAGON_TIGER_API_URL` 指定兼容的代理地址；板块排行按行业/概念缓存 5 分钟，龙虎榜按股票缓存 30 分钟。

自选股：

```bash
./dist/astock add 贵州茅台 宁德时代
./dist/astock remove 宁德时代
./dist/astock list
./dist/astock                 # 读取已保存自选并持续刷新
```

兼容原来的简写：`astock 600519`、`astock --moyu 600519`、`astock --add 600519`。

## 接入 TradingAgents-Astock

`watch` 完全不需要 Python。只有 `analyze` 需要：

1. 一个可工作的 `tradingagents-astock` checkout；
2. 该项目的 Python 3.10+ 虚拟环境；
3. 对应模型供应商的 API 凭证。

本机已有项目时，先自检：

```bash
./dist/astock doctor --repo /Users/wenzhe/tradingagents-astock
```

运行一次分析：

```bash
./dist/astock analyze --repo /Users/wenzhe/tradingagents-astock 600519
```

也可以固定环境，之后不再传路径：

```bash
export ASTOCK_TRADINGAGENTS_HOME=/Users/wenzhe/tradingagents-astock
export ASTOCK_TRADINGAGENTS_PYTHON=/Users/wenzhe/tradingagents-astock/.venv/bin/python
./dist/astock analyze 贵州茅台
```

覆盖模型配置的例子：

```bash
./dist/astock analyze \
  --provider deepseek \
  --deep-model deepseek-chat \
  --quick-model deepseek-chat \
  600519
```

未显式传入时，provider、模型和 backend URL 均沿用 TradingAgents-Astock 的
`DEFAULT_CONFIG` 与环境变量。模型密钥不会复制进本项目，桥接进程只继承当前环境并读取
TradingAgents 项目自己的 `.env`。

一次完整分析通常会产生 30–50 次模型调用，可能耗时数分钟并产生 API 费用。`doctor`
只验证本地环境，不发起正式分析。

## 分析结果

结果默认保存到：

```text
~/.local/share/astock-workbench/reports/<股票代码>/<分析ID>/
├── analysis.json
└── report.md
```

查看历史：

```bash
./dist/astock reports list
./dist/astock reports show 600519
```

稳定 JSON 契约包含：标的、分析日期、五档组合评级、provider/model、数据供应商、全部
分析师报告、研究/风险辩论结论和最终组合决策。后续策略、回测和模拟盘只消费该契约，
不直接导入 LangGraph。

## 数据和风险边界

- 实时表格是公开 Level-1 快照，不等同于交易所直连行情，也不保证逐笔或毫秒级实时性。
- 个股和指数资金流来自东方财富公开口径，是市场行为参考；主接口不可用时会回退到公开延迟节点。
- 指数资金分别对应三大指数的主力净额，指数成分股存在重叠，不能把三项相加当作全市场总额。
- 技术判断必须使用历史 K 线和量能；实时快照只用于展示与分析时点提示。
- 财务、公告、减持、合同等正式结论应回到交易所或巨潮资讯原文核验。
- 报告输出是情景研究信号，不是确定性买卖建议；应明确触发条件、仓位和失效条件。
- 研究、回测、模拟撮合和未来实盘 Broker 是不同进程/接口，实盘层必须再经过确定性风控与人工确认。
- 回测实现时必须固定复权口径，并处理未来函数、幸存者偏差、点时股票池、手续费和滑点。

## 分发

构建 macOS Intel、Apple Silicon 和通用二进制：

```bash
make dist-macos
```

实时看盘只需把匹配朋友机器架构的单个二进制发给他：

```text
dist/astock-darwin-arm64       Apple Silicon
dist/astock-darwin-amd64       Intel Mac
dist/astock-darwin-universal   两种 Mac 通用，但体积约等于两者之和
```

朋友首次运行若被 macOS Gatekeeper 拦截，可自行执行：

```bash
xattr -d com.apple.quarantine ./astock-darwin-arm64
chmod +x ./astock-darwin-arm64
./astock-darwin-arm64 watch 600519
```

如果朋友还要使用 `analyze`，仅发送 Go 二进制不够；还需给他安装
TradingAgents-Astock、Python 环境和自己的模型 API Key。不要分享你的 `.env` 或 API Key。

## 路线图

1. 历史 K 线仓储和统一复权数据契约；
2. 确定性指标策略与 TradingAgents 研究信号合成；
3. Backtrader 适配器、走样本外和滚动回测；
4. 遵守 T+1、整手、涨跌停和停牌规则的模拟撮合；
5. 只读观察期、风控门禁、人工确认后的券商实盘适配器。

详细边界见 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)。

## 许可

本项目使用 MIT License。TradingAgents-Astock 作为独立可选依赖，使用 Apache-2.0；详见
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。
