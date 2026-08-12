# Changelog

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
