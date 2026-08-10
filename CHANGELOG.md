# Changelog

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
