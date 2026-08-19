#!/usr/bin/env python3
"""JSON-lines bridge for the optional MIT-licensed tdxrs TCP client.

Go owns the application interfaces and HTTP fallback policy. This process
keeps the TDX connections alive so a refresh does not create a new TCP
connection for every stock. stdout is reserved for JSON responses; package
diagnostics are intentionally not printed there.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
from datetime import datetime


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--server", default="")
    return parser.parse_args()


def market_code(symbol: str) -> int:
    return 1 if symbol.lower().startswith("sh") else 0


def code_only(symbol: str) -> str:
    value = str(symbol).lower()
    return value[2:] if value[:2] in ("sh", "sz", "bj") else value


def finite(value, default=None):
    try:
        result = float(value)
    except (TypeError, ValueError):
        return default
    if result != result or result in (float("inf"), float("-inf")):
        return default
    return result


def quote_item(item):
    price = finite(item.get("price"), 0.0)
    previous = finite(item.get("last_close"), 0.0)
    delta = price - previous if price and previous else 0.0
    percent = delta / previous * 100 if previous else 0.0
    # TDX quote packets do not carry a universally reliable timestamp. The
    # bridge response time is more honest than inventing an exchange time.
    quote_time = datetime.now().strftime("%Y-%m-%d %H:%M:%S")
    bids = []
    asks = []
    for level in range(1, 6):
        bids.append({
            "level": level,
            "price": "%.2f" % finite(item.get("bid%d" % level), 0.0),
            "volume": "%.0f" % finite(item.get("bid_vol%d" % level), 0.0),
        })
        asks.append({
            "level": level,
            "price": "%.2f" % finite(item.get("ask%d" % level), 0.0),
            "volume": "%.0f" % finite(item.get("ask_vol%d" % level), 0.0),
        })
    return {
        "symbol": ("sh" if int(item.get("market", 0)) == 1 else "sz") + str(item.get("code", "")),
        "code": str(item.get("code", "")),
        "current": "%.2f" % price,
        "previous_close": "%.2f" % previous,
        "open": "%.2f" % finite(item.get("open"), 0.0),
        "high": "%.2f" % finite(item.get("high"), 0.0),
        "low": "%.2f" % finite(item.get("low"), 0.0),
        "quote_time": quote_time,
        "delta": delta,
        "percent": percent,
        # tdxrs reports amount in yuan; Go's Quote.Amount uses ten-thousand yuan.
        "amount": finite(item.get("amount"), 0.0) / 10000,
        "volume": finite(item.get("vol"), 0.0),
        "bids": bids,
        "asks": asks,
    }


def bar_item(item, symbol):
    date = str(item.get("datetime") or "")[:10]
    if not date:
        date = "%04d-%02d-%02d" % (
            int(item.get("year", 0)), int(item.get("month", 0)), int(item.get("day", 0))
        )
    return {
        "symbol": symbol,
        "source": "通达信TCP",
        "date": date,
        "open": finite(item.get("open"), 0.0),
        "close": finite(item.get("close"), 0.0),
        "high": finite(item.get("high"), 0.0),
        "low": finite(item.get("low"), 0.0),
        "volume": finite(item.get("vol"), 0.0),
        "amount_yuan": finite(item.get("amount"), 0.0),
    }


def minute_items(rows, symbol):
    trade_date = datetime.now().strftime("%Y-%m-%d")
    points = []
    cumulative_volume = 0.0
    cumulative_amount = 0.0
    # Current and historical minute APIs may use opposite orders on different
    # nodes. Sorting by the HH:MM field keeps the chart deterministic.
    for item in sorted(list(rows or []), key=lambda row: str(row.get("time") or "")):
        value = finite(item.get("price"), 0.0)
        average = finite(item.get("avg_price"), value)
        volume = finite(item.get("vol"), 0.0)
        clock = str(item.get("time") or "")
        if value <= 0 or volume < 0 or len(clock) != 5 or ":" not in clock:
            continue
        cumulative_volume += volume
        # TDX volume is in lots (手); this gives a useful yuan estimate when
        # the packet does not carry a cumulative amount field.
        amount = value * volume * 100
        cumulative_amount += amount
        points.append({
            "symbol": symbol,
            "source": "通达信TCP",
            "trade_date": trade_date,
            "time": clock,
            "price": value,
            "average": average if average and average > 0 else value,
            "volume": volume,
            "amount_yuan": amount,
            "cumulative_volume": cumulative_volume,
            "cumulative_amount_yuan": cumulative_amount,
        })
    return points


class TDXBackend:
    """Use SmartClient for quote/K-line and HqClient for minute packets.

    SmartClient retries away from nodes that return empty quote packets. A
    configured server intentionally uses HqClient only, so the user's node
    choice remains authoritative and the Go layer can fall back on failure.
    """

    def __init__(self, configured_server: str):
        from tdxrs import TdxHqClient, TdxSmartClient

        self.smart = None
        self.hq = None
        self.server = configured_server.strip()
        errors = []
        if self.server:
            host, port = self.server.rsplit(":", 1) if ":" in self.server else ("", "")
            if not host or not port:
                raise RuntimeError("ASTOCK_TDX_SERVER 格式应为 host:port")
            try:
                self.hq = TdxHqClient()
                connected = self.hq.connect(host.strip(), int(port), timeout=2.0)
            except Exception as exc:
                connected = False
                errors.append(str(exc))
            if not connected:
                self.hq = None
                errors.append("指定节点连接失败")
        else:
            try:
                self.smart = TdxSmartClient()
                smart_connected = self.smart.connect_to_any(timeout=2.0)
            except Exception as exc:
                smart_connected = False
                errors.append(str(exc))
            if not smart_connected:
                self.smart = None
                errors.append("智能行情节点连接失败")
            try:
                self.hq = TdxHqClient()
                hq_connected = self.hq.connect_to_any(timeout=2.0)
            except Exception as exc:
                hq_connected = False
                errors.append(str(exc))
            if not hq_connected:
                self.hq = None
                errors.append("K线/分时节点连接失败")
        if self.smart is None and self.hq is None:
            raise RuntimeError("没有可用通达信 TCP 节点；" + ";".join(errors))

    @staticmethod
    def _pairs(symbols):
        return [(market_code(symbol), code_only(symbol)) for symbol in symbols]

    def _client(self):
        return self.hq if self.server else self.smart

    def quotes(self, symbols):
        clients = [self._client(), self.hq if not self.server else None]
        errors = []
        for client in clients:
            if client is None:
                continue
            try:
                rows = client.get_security_quotes(self._pairs(symbols)) or []
                if rows:
                    return [quote_item(row) for row in rows]
                errors.append("行情节点返回空数据")
            except Exception as exc:
                errors.append(str(exc))
        raise RuntimeError("；".join(errors) or "TDX 未返回行情")

    def bars(self, symbol, count):
        clients = [self._client(), self.hq if not self.server else None]
        errors = []
        for client in clients:
            if client is None:
                continue
            try:
                if client is self.hq:
                    rows = client.get_security_bars_all(9, market_code(symbol), code_only(symbol), count, 0) or []
                else:
                    rows = client.get_security_bars(9, market_code(symbol), code_only(symbol), 0, count, 0) or []
                if len(rows) >= min(60, count):
                    return [bar_item(row, symbol) for row in rows]
                errors.append("K线节点仅返回%d根" % len(rows))
            except Exception as exc:
                errors.append(str(exc))
        raise RuntimeError("；".join(errors) or "TDX 未返回日 K")

    def minutes(self, symbol):
        if self.hq is None:
            raise RuntimeError("TDX 分时连接不可用")
        market = market_code(symbol)
        code = code_only(symbol)
        today = int(datetime.now().strftime("%Y%m%d"))
        errors = []
        try:
            rows = self.hq.get_history_minute_time_data(market, code, today) or []
        except Exception as exc:
            rows = []
            errors.append(str(exc))
        if not rows:
            try:
                rows = self.hq.get_minute_time_data(market, code) or []
            except Exception as exc:
                errors.append(str(exc))
                rows = []
        if not rows:
            raise RuntimeError("TDX 未返回分时行情；" + "；".join(errors))
        return minute_items(rows, symbol)

    def close(self):
        for client in (self.smart, self.hq):
            if client is not None:
                try:
                    client.disconnect()
                except Exception:
                    pass


def dispatch(backend, request):
    method = request.get("method")
    params = request.get("params") or {}
    if method == "quote":
        return backend.quotes([str(value) for value in params.get("symbols", [])])
    if method == "bars":
        symbol = str(params.get("symbol", ""))
        count = max(1, min(800, int(params.get("count", 300))))
        return backend.bars(symbol, count)
    if method in ("minutes", "minute"):
        return backend.minutes(str(params.get("symbol", "")))
    if method == "status":
        return {"status": "connected", "server": backend.server or "auto"}
    raise RuntimeError("不支持的 TDX 方法: %s" % method)


def main():
    args = parse_args()
    backend = None
    try:
        backend = TDXBackend(args.server or os.getenv("ASTOCK_TDX_SERVER", ""))
        print(json.dumps({"id": 0, "ok": True, "result": {"server": backend.server or "auto"}}, ensure_ascii=False), flush=True)
    except Exception as exc:
        print(json.dumps({"id": 0, "ok": False, "error": str(exc)}, ensure_ascii=False), flush=True)
        return 1
    try:
        for line in sys.stdin:
            request = {}
            try:
                request = json.loads(line)
                result = dispatch(backend, request)
                response = {"id": request.get("id"), "ok": True, "result": result}
            except Exception as exc:
                response = {"id": request.get("id"), "ok": False, "error": str(exc)}
            print(json.dumps(response, ensure_ascii=False, separators=(",", ":")), flush=True)
    finally:
        if backend is not None:
            backend.close()


if __name__ == "__main__":
    raise SystemExit(main())
