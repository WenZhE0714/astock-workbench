# Changelog

## 0.21.0

- 个股研判与市场扫描新增冻结信息凭证快照：近30日公告索引、近7日财经新闻和近90日券商研报统一分级、去重、过滤未来数据并编号为 `[E##]`。
- 报告严格区分 A 级已核验官方披露正文、B 级专业观点、C 级公告/新闻待核线索和 D 级市场情绪；东方财富公告索引不会冒充 A 级事实，券商评级和媒体内容不能单独支撑买卖结论。
- 多角色 Agent 共用同一份带哈希的证据快照，外部事件或观点必须引用快照内的凭证编号；无引用或引用未知编号时自动回退到确定性报告。
- 市场扫描只为最终观察池前5只候选采集券商观点，避免对全市场逐股请求；报告与快照同时保存独立的 `evidence.json`，正文追加来源、作者、时间和原文入口。

## 0.20.0

- 将 `x`、`c`、`s` 接入共享事实快照的多角色 Agent 工作流：角色并行、独立十分钟超时、失败隔离，再由主 Agent 依据不可变事实综合。
- 个股研判增加技术量价、资金板块、事件风险和反方审计角色；AI 咨询增加技术、资金板块和事件风险角色；市场报告增加指数承接、板块轮动、候选筛选和风险审计角色。
- 报告与 AI 咨询保存角色状态、耗时、成功数和事实快照哈希；旧归档可兼容读取，报告页显示多Agent成功比例。
- 增加行情时间、日K截止日、缓存来源、盘中未完成日K和缺失字段警告；盘中技术分析自动排除尚未收盘的当日K线。
- 1/3/5分钟资金增量增加采样时间，超过两分钟的旧样本不再进入新报告；AI咨询在Agent或主综合失败时仍保存本轮事实与角色状态，并返回明确的确定性降级答复。
- 市场扫描没有满足条件的候选时保留大盘与板块报告并明确观察池为空，不为凑数降低量价和资金门槛。
- 持续策略优化继续由3个提案Agent与确定性Go引擎协作，并收紧重复滚动窗口、非有限门禁和弱参数邻域的晋级检查。
- AI咨询历史改为最新一轮置顶显示，持久化和发送给Agent的上下文仍保持时间正序。

## 0.19.1

- Pre-qualify out-of-range technical levels in the structured facts sent to
  stock-report and AI-consultation agents, so cross-session levels cannot be
  mistaken for same-day triggers.
- Preserve an AI report when its second draft still misses a price-boundary
  label by deterministically annotating only the offending actionable levels
  and validating the corrected text again.
- Validate a complete semantic sentence before splitting it into clauses, so
  one trailing cross-session declaration can cover multiple levels without
  misclassifying a historical holding cost as a sell trigger.
- Replace internal price-validator details in deterministic fallback headers
  with a concise user-facing boundary-correction status.

## 0.19.0

- Add reproducible continuous-research manifests with canonical candidate-set
  and configuration SHA-256 fingerprints, persisted as `manifest.json`.
- Add deterministic data-quality gates for rolling coverage, no-future-data,
  adjustment consistency and holdout data-source completeness, while reporting
  point-in-time pool and benchmark limitations as explicit warnings.
- Score independent Agent consensus and nearby parameter stability, and keep an
  isolated optimum in research even when its single backtest score is highest.
- Require a completely new, non-overlapping three-month holdout before another
  cycle can inherit the previous locked parameters and bounded reflection.
- Expose quality checks, fingerprints, consensus, parameter neighborhoods,
  holdout/stress facts and prior lessons in the CLI, strategy center, reports
  and immutable supervisor review context.
- Make candidate stability scoring independent of iteration order, isolate
  individual candidate failures, and retain legacy holdout protection by
  falling back to archived data cutoffs.
- Document the adopted experiment discipline from TradingAgents-Astock, Qlib,
  RQAlpha, vn.py and FinRL without adding them as runtime dependencies.

## 0.18.0

- Add a Go-supervised multi-agent continuous optimization workflow: three
  isolated Codex sub-agents propose bounded, structured candidates while the
  deterministic engine exclusively owns validation, ranking and promotion.
- Add breakout, trend-reclaim and moving-average pullback entry modes within
  the existing auditable daily technical strategy family.
- Evaluate candidates across multiple rolling train/validation folds, lock one
  candidate before a final unseen holdout, then run doubled-cost and profit
  concentration stress checks.
- Classify results conservatively as research candidates or shadow-observation
  candidates; no result automatically changes live signals or submits trades.
- Persist continuous experiments separately under `backtests/continuous`,
  including agent runs, candidates, holdout/stress artifacts and supervisor
  review, and expose them through both `backtest continuous` and the `t` center.

## 0.17.3

- Increase the default Codex timeout from five to ten minutes, give stock
  report generation and its automatic price-boundary correction independent
  timeout budgets, and report timeouts explicitly without UTF-8 corruption or
  leaking partial generated prose into the error message.
- Allow market reports, stock reports, AI consultation and strategy research
  to run independently in the background; only duplicate starts of the same
  task are rejected.
- Show every active, completed or failed background task on its own status
  line so one failure no longer hides the progress of other tasks.
- Add report history pages grouped by date and then generation time for both
  market and per-stock reports; `r` and `o` still open the latest report, while
  `h` inside a report opens its historical archive.

## 0.17.2

- Persist the latest 100 AI consultation turns per stock and restore them from
  the `x` consultation page after switching stocks or restarting the program;
  only the latest six turns are sent back to the Agent as context.
- Add the quote-date limit-up and limit-down interval to stock analysis facts
  and make it a hard boundary for both live consultation and stock reports.
- Automatically retry an AI draft that describes an out-of-range structural
  level as a same-day trigger, then reject it if the corrected draft still
  violates the boundary; deterministic fallback reports label such levels as
  cross-session observations.

## 0.17.1

- Add semantic color hierarchy to the strategy research center while keeping
  `--no-color` and `--moyu` strictly ANSI-free.
- Highlight the title, context, date splits, settings, statuses and selected
  action with distinct restrained colors instead of a flat text-only page.
- Keep `t` strategy research on the same operation-bar row as `x/c/o/s/r`
  across the main watchlist, rankings and fund-radar pages.

## 0.17.0

- Add `t` as a strategy-research entry from the live watchlist, stock detail,
  market rankings, fund radar, board funds, AI chat and report pages.
- Add an in-dashboard strategy center for running a backtest or a bounded
  train/validation/out-of-sample optimization without leaving live quotes.
- Add page settings for current-stock/current-list scope, backtest length,
  optimization split, candidate count, validation trade gate and read-only AI.
- Show persisted backtest and optimization history inside the dashboard,
  including metrics, parameters, candidate ranking, OOS results and trades.
- Let users drill from a backtest into every entry/exit signal, next-open fill,
  fee, return, MFE/MAE and exit reason without using a shell subcommand.
- Report candidate and OOS progress through the dashboard event loop while
  keeping strategy facts immutable and all market quote polling responsive.

## 0.16.0

- Add bounded deterministic strategy optimization across strictly ordered,
  non-overlapping training, validation and one-time out-of-sample periods.
- Rank candidates only from training and validation metrics with hard gates for
  trade count, drawdown, validation return, finite values and performance gaps;
  never use out-of-sample results to reselect parameters.
- Reuse historical ranges in memory while evaluating candidates so a 30-item
  parameter search does not become 30 duplicate HTTP requests per period.
- Add `backtest optimize/optimize-list/optimize-show`, persist candidate ranks,
  selected parameters and full trade/equity artifacts for all selected splits
  under a separate `backtests/optimizations` archive.
- Add an optional ephemeral read-only Codex review after deterministic
  selection and out-of-sample evaluation; AI failures preserve the experiment,
  and AI cannot change fills, metrics, gates or parameter ranking.

## 0.15.0

- Add an auditable daily A-share backtest engine with next-open execution,
  T+1, 100-share lots, commissions, stamp duty, transfer fees and slippage.
- Add a deterministic volume-breakout/trend strategy with persisted parameters,
  signal evidence, entry/exit fills, MFE/MAE and exit reasons for every trade.
- Add `backtest run/list/show/trades/trade` commands and archive each run as
  Markdown, JSON, JSONL trade records and a CSV equity/drawdown curve.
- Fix the data basis to unadjusted daily bars for the first engine, explicitly
  warning about corporate actions, static-pool survivor bias and daily-bar
  limit-up fill limitations instead of overstating backtest accuracy.

## 0.14.0

- Add a `y` industry-fund dashboard with the five largest main-fund inflows
  and five largest outflows in one scrollable view.
- Show three high-liquidity core stocks for every board, explicitly defined as
  the board constituents with the highest turnover amount; display code,
  change, price speed and individual main-fund net amount.
- Load the dashboard in a background goroutine so quotes keep refreshing, cap
  automatic refreshes at once per minute while visible, and bound constituent
  lookups to four concurrent requests.
- Preserve the last successful dashboard when a refresh fails, and retain the
  other nine board blocks when one constituent lookup is unavailable.

## 0.13.0

- Add `x` live AI consultation for the selected stock, automatically passing
  current quotes, unadjusted daily technicals, cumulative and 1/3/5-minute
  fund evidence, industry/board flow, announcements and news clues to a
  read-only ephemeral Codex Agent.
- Keep AI collection and synthesis in a background goroutine so live quotes,
  rankings and fund sampling continue; notify on completion and provide a
  scrollable in-memory conversation with `x` follow-up questions.
- Require conditional buy/sell answers with cited evidence, existing technical
  levels, invalidation and risk; the Agent cannot place orders, read files or
  search the network.
- Poll quotes and rankings from 09:15 during call auction, label 09:15-09:25 as
  `集合竞价` and 09:25-09:30 as `开盘等待`, while keeping strict intraday
  screening limited to continuous trading.
- Show Eastmoney `f22` price speed after the quote change column in standard
  and compact watchlists, without substituting the daily change percentage.

## 0.12.3

- Sort fund-radar rows by one-minute main-fund change descending, placing
  stocks without a complete one-minute sample after sampled stocks.
- Keep the selected stock stable while the fund radar is dynamically sorted.
- Show industry direction and the full industry name without squeezing fund
  summaries into the same column or prefixing missing data with a misleading
  `--` marker; show the board net amount in a separate wide-terminal column.
- Expand the industry-flow request from 100 to 500 rows to reduce unmatched
  stock industries.

## 0.12.2

- Preserve printable shortcut characters such as `g`, `j`, `k`, `b`, `q`,
  `[` and `]` while a code/name command is active, so pinyin and group names
  can be entered without navigation shortcuts swallowing individual keys.
- Keep the same printable keys as navigation and quit shortcuts outside text
  input, while control-key variants remain commands rather than input text.

## 0.12.1

- Retry transient Eastmoney/Tencent daily-K failures and cache successful
  unadjusted series for up to 14 days, preserving evidence-based price levels
  during temporary empty responses or HTTP 501 errors.
- Keep optional Dragon-Tiger, news, board and announcement failures as scoped
  warnings instead of appending them to the fatal daily-K error message.
- Allow market scans outside continuous trading, or while Eastmoney main-flow
  fields are unavailable, to build an explicitly low-confidence observation
  pool from liquidity, board breadth, leadership and recent daily structure.
- Preserve strict positive-price/positive-flow screening during continuous
  trading when flow data is available, and never serialize missing flow as a
  false flat or inflow conclusion.

## 0.12.0

- Add `c` background stock analysis and `o` latest-report viewing from the
  watchlist, market rankings, stock detail and fund radar without stopping
  live quotes.
- Add `astock stock-report [--full] [--no-ai]` for the same pipeline outside
  the interactive dashboard.
- Combine Tencent quotes, unadjusted daily technical structure, Eastmoney
  cumulative and rolling fund evidence, related-board breadth/ranks, recent
  Dragon-Tiger records, announcement indexes and third-party news clues.
- Require every bullish/bearish conclusion and observation point to reuse
  deterministic MA/structure/volume evidence; announcement and news titles
  remain unverified clues and cannot support invented facts.
- Persist per-stock Markdown, structured snapshots and AI metadata under
  `stock-reports/<code>/<timestamp>`, with a deterministic fallback when
  Codex is unavailable.

## 0.11.1

- Warm the current watchlist's individual and industry fund samples in the
  background as soon as the live dashboard starts.
- Keep background sampling separate from radar visibility, so opening `v`
  reuses accumulated 1/3/5-minute history instead of starting from zero.
- Rebind the warm pool after watchlist group changes or additions/removals;
  closed markets still wait for the next trading session unless manually
  refreshed from the radar.

## 0.11.0

- Add non-blocking `s` market-report generation to the live dashboard, with
  persistent progress while quotes, rankings and fund monitoring keep running.
- Add `r` report viewing with arrow-key scrolling, `[`/`]` paging and Escape
  return to the unchanged live view.
- Scan broad-market turnover, index MA/volume structure, industry breadth and
  main-fund flow, then preserve five hot-board leaders in a diversified
  A/B/C observation pool with chase, weak-board and announcement risk flags.
- Run Codex in ephemeral, read-only and non-interactive mode on structured
  market facts only; automatically persist a deterministic report when Codex
  is unavailable or times out.
- Add `astock scan`, `--no-ai` and `--full`, with timestamped Markdown,
  snapshot JSON and metadata archives under the application data directory.

## 0.10.0

- Add a `v` main-fund radar for the current watchlist group or active market
  ranking, with detail navigation that returns to the radar without changing
  recently viewed history.
- Sample cumulative main-fund snapshots every 10 seconds during trading, keep
  six minutes in memory, and derive rolling 1/3/5-minute net-flow changes.
- Combine stock flow, current price movement and 60-second industry flow into
  behavior labels such as reversal, acceleration, divergence and resonance;
  labels remain observational and never become deterministic trade commands.
- Keep ranking membership synchronized, prevent overlapping radar requests,
  preserve the last successful samples on failure, and reject stale responses
  after the radar source changes or closes.
- Add a height-aware and width-responsive radar table with manual refresh
  outside trading hours.

## 0.9.0

- Add top-20 Shanghai/Shenzhen A-share gainers, losers and rapid-rise rankings
  with Eastmoney industry classification on every row.
- Add `1`/`2`/`3` ranking shortcuts, height-aware selection, direct detail
  navigation and return-to-ranking behavior.
- Refresh the active ranking during trading at the configured quote interval,
  defaulting to once per second while preventing overlapping requests.
- Split the base operation footer into two lines so market rankings remain
  visible without truncating the existing watchlist shortcuts.

## 0.8.0

- Persist the 100 most recently viewed stocks opened through the interactive
  view command, with duplicate views moved back to the front.
- Add `h` history selection with newest-first ordering and direct detail
  restoration without changing the saved watchlist.

## 0.7.0

- Add an unadjusted daily K-line contract with Eastmoney as the primary source
  and an automatic Tencent fallback when the primary history is unavailable.
- Add deterministic daily swing signals based on MA5/20/60, MACD, RSI14,
  prior-20-day structure and volume confirmation.
- Show bullish, bearish or range-bound bias together with conditional buy/sell
  triggers, invalidation, support/resistance basis and a staged position plan.
- Map directional signals to `CALL-like` / `PUT-like` labels while explicitly
  distinguishing them from option contracts, short selling and automatic
  trading instructions.
- Load technical signals asynchronously on the stock detail screen and cache
  them for five minutes during trading, without resuming polling after close.
- Add `m` group assignment for the selected stock, with multi-select checkboxes
  and explicit current/add/remove membership states before saving.
- Preserve existing per-group order during assignment and keep a stock in the
  default group when every group is unchecked, preventing accidental loss.
- Show one compact turnover row labelled Shanghai-Shenzhen while calculating
  the Shanghai-Shenzhen-Beijing A-share total without double-counting Shenzhen
  Component or ChiNext constituents.
- Fetch the previous session's Shanghai Composite, Shenzhen Composite and BSE
  50 five-minute turnover from Sina and sum exact amount fields by trade date;
  no Tushare token or volume-based estimation is required.
- Keep current Tencent and previous Sina values on the same live-quote scope,
  and hide the comparison before the quote date advances on a new session.
- Remove the Tushare runtime adapter and its token configuration.

## 0.6.0

- Add backward-compatible ordered watchlist groups in the existing watchlist
  file, with an interactive group picker, group creation/deletion and current
  group visibility in the dashboard.
- Add two-stage reorder mode: select a stock, press Enter to pick it, move it
  with navigation keys, then press Enter to persist or Escape to restore.
- Make interactive add/remove operations group-aware; deleting a group moves
  stocks unique to that group into the default group to avoid data loss.

## 0.5.0

- Classify related industry/concept boards as hot, divergent-hot, active,
  normal or cold using same-type top-100 change, main-flow and turnover ranks
  together with board breadth.
- Show the ranking evidence, turnover and advancing/declining breadth for each
  related board; rank snapshots are cached for five minutes.
- Show the latest three Eastmoney Dragon-Tiger list records from the last 30
  days, including trigger reason, buy/sell/net amounts, deal ratio, turnover,
  seat tag and available post-listing returns.

## 0.4.0

- Show the six most relevant Eastmoney industry/concept boards in live stock
  detail, including board change, main-fund flow, ratio and leading stock.
- Summarize whether related-board funds are mostly flowing in, flowing out or
  diverging, and compare the stock's main-fund direction with its boards.
- Cache board snapshots per stock and refresh them at most once per minute
  during trading sessions; closed markets remain interactive without polling.

## 0.3.5

- Add optional Tushare Pro `index_daily` fallback for previous-day index
  turnover when anonymous historical endpoints are unavailable.

## 0.3.4

- Hide the previous-day turnover comparison when historical turnover data is
  unavailable; the current total turnover remains visible without a misleading
  placeholder.

## 0.3.3

- Compare current Shanghai-plus-Shenzhen turnover with the previous trading
  day's unadjusted daily turnover and show the absolute and percentage change.
- Keep the historical amount request low-frequency and independent from the
  one-second quote refresh loop.

## 0.3.2

- Replace fund-flow words with compact direction arrows: `↑` inflow, `↓`
  outflow, and `→` flat.
- Add standard-board limit-up and limit-down columns and restore color coding
  for standard live lists, index summaries, and fund-flow directions.
- Show Shanghai-plus-Shenzhen total turnover using the two exchange index
  turnover fields; ChiNext is not added because it is a Shenzhen subset.

## 0.3.1

- Show individual-stock main-fund inflow/outflow in standard list, standard
  detail, `--once`, and `--moyu` views.
- Fetch main-fund flow snapshots for the Shanghai Composite, Shenzhen
  Component, and ChiNext alongside watched stocks.
- Add a separate index-fund-flow summary; the three overlapping index values
  are intentionally not summed into a fake whole-market total.

## 0.3.0

- Replace line scrolling in the live dashboard with stock selection: arrow
  keys or `j`/`k` select a stock, Enter opens its full dashboard, and Escape
  returns to the list.
- Show Shanghai Composite, Shenzhen Component, ChiNext, market status, and the
  last successful quote refresh time in both standard and `--moyu` modes.
- Poll quotes only during the Shanghai/Shenzhen morning and afternoon sessions;
  keep the interface interactive without network polling before open, at lunch,
  after close, and on weekends.
- Add Eastmoney main-fund inflow/outflow to `--moyu`, refreshed at most once per
  minute with an automatic public delayed-endpoint fallback.
- Keep live list rows within the terminal height and preserve absolute-row,
  no-newline rendering in list and detail views.

## 0.2.6

- Add a height-aware viewport when the live dashboard exceeds the terminal.
- Support arrow keys, `j`/`k`, Page Up/Page Down, `b`/Space, `g`/`G`, and
  `q` for terminal navigation.
- Keep the current scroll position stable while quote data refreshes.
- Recalculate the viewport after terminal resizes without addressing rows below
  the new screen height.
- Preserve absolute-row drawing with no line-feed or carriage-return output, so
  navigation and refreshes do not grow the main terminal scrollback.

## 0.2.5

- Draw every dashboard row with an absolute terminal coordinate.
- Remove all line-feed and carriage-return output from continuous refreshes.
- Avoid iTerm2 alternate-screen scrollback growth caused by repeated screen
  erase and newline-based frame drawing.

## 0.2.4

- Move continuous dashboards to the terminal alternate screen buffer.
- Fix repainting when the command starts near the bottom of the main terminal
  and the first multi-stock frame itself causes the main screen to scroll.
- Restore the original shell screen and cursor when the dashboard exits.

## 0.2.3

- Replace relative row-count repainting with an absolute saved cursor anchor.
- Fix multi-stock dashboards drifting downward when terminal glyph widths or
  soft wrapping differ from the renderer's logical line count.
- Clear and redraw only the dashboard region from its original start position.

## 0.2.2

- Fix terminal scrollback growing on every live refresh.
- Keep the cursor on the dashboard's final row during repaint and emit a
  newline only once when the program exits.
- Correct cursor movement when the rendered panel changes height.
- Detect the actual terminal width and reserve the rightmost column to prevent
  automatic line wrapping from invalidating the repaint area.

## 0.2.1

- Change the default real-time quote refresh interval from 3 seconds to 1 second.
- Keep `-i` / `--interval` configurable from 1 to 3600 seconds.

## 0.2.0

- Redesign the standard quote view as a responsive terminal dashboard.
- Always show best bid and best ask with order volume outside `--moyu` mode.
- Add an intraday low/current/high price rail.
- Add amplitude, volume ratio, average price, PE(TTM), PB, total/float market cap,
  and limit-up/limit-down prices.
- Keep `--depth` as the full five-level order-book expansion.
- Preserve the compact, colorless `--moyu` and pinyin views unchanged.

## 0.1.0

- Initial Go workbench with real-time quotes, watchlists and TradingAgents bridge.
