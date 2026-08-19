# Third-party notices

## astock-go

The real-time quote, symbol resolution, watchlist, pinyin table and in-place
terminal rendering modules are derived from `/Users/wenzhe/astock-go`.

- License: MIT
- Copyright: 2026 astock-go contributors

The MIT notice is preserved in this repository's license and this notice.

## TradingAgents-Astock

`astock-workbench` does not vendor TradingAgents-Astock source code. The
optional `analyze` command invokes a separately installed checkout through a
subprocess adapter.

- Project: https://github.com/simonlin1212/tradingagents-astock
- License: Apache License 2.0
- Original project: TauricResearch/TradingAgents

Users who redistribute or modify TradingAgents-Astock must comply with its own
`LICENSE`, `NOTICE`, and `CHANGES_FROM_UPSTREAM.md` files.

## Vue.js

- Project: https://github.com/vuejs/core
- License: MIT
- Usage: bundled browser runtime for the local read-only Web interface

## Vite

- Project: https://github.com/vitejs/vite
- License: MIT
- Usage: Web interface build tool; not used by the running Go service

## tdxrs

`tdxrs` is an optional, separately installed Python dependency used only when
the user selects the TongdaXin TCP market source.

- Project: https://github.com/jiangtaovan/tdxrs
- Version tested: 0.6.7
- License: MIT
- Usage: TongdaXin TCP quote, unadjusted daily-bar and intraday packet client

## Go dependencies

- `github.com/mozillazg/go-pinyin` — MIT
- `golang.org/x/sys` — BSD-3-Clause
- `golang.org/x/term` — BSD-3-Clause
- `golang.org/x/text` — BSD-3-Clause
