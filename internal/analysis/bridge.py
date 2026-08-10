#!/usr/bin/env python3
"""Stable JSON bridge from astock-workbench to TradingAgents-Astock.

The bridge deliberately lives behind a subprocess boundary. The Go terminal
does not import LangGraph/LLM dependencies, and a research failure cannot take
down the real-time quote loop.
"""

from __future__ import annotations

import argparse
import copy
import importlib.metadata
import json
import os
from pathlib import Path
import sys
import tempfile
import time
import traceback
import uuid
from datetime import datetime, timezone


SCHEMA_VERSION = 1
DISCLAIMER = (
    "本报告用于研究与策略讨论，不构成确定性买卖指令。请结合数据时点、"
    "触发条件、仓位上限与失效条件独立决策；研究信号不得绕过风险控制直接下单。"
)

CREDENTIALS = {
    "openai": "OPENAI_API_KEY",
    "anthropic": "ANTHROPIC_API_KEY",
    "minimax": "MINIMAX_API_KEY",
    "deepseek": "DEEPSEEK_API_KEY",
    "qwen": "DASHSCOPE_API_KEY",
    "glm": "ZHIPU_API_KEY",
    "google": "GOOGLE_API_KEY",
    "xai": "XAI_API_KEY",
    "openrouter": "OPENROUTER_API_KEY",
    "ollama": "",
}


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repo", required=True)
    parser.add_argument("--output", required=True)
    parser.add_argument("--work-dir", required=True)
    parser.add_argument("--ticker")
    parser.add_argument("--date")
    parser.add_argument("--provider")
    parser.add_argument("--deep-model")
    parser.add_argument("--quick-model")
    parser.add_argument("--backend-url")
    parser.add_argument("--checkpoint", action="store_true")
    parser.add_argument("--check", action="store_true")
    return parser.parse_args()


def write_json(path: Path, payload: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    handle = tempfile.NamedTemporaryFile(
        mode="w", encoding="utf-8", dir=str(path.parent),
        prefix=path.name + ".", suffix=".tmp", delete=False,
    )
    temporary = Path(handle.name)
    try:
        with handle:
            json.dump(payload, handle, ensure_ascii=False, indent=2)
            handle.write("\n")
        os.replace(str(temporary), str(path))
    finally:
        if temporary.exists():
            temporary.unlink()


def package_version() -> str:
    for name in ("tradingagents", "TradingAgents-Astock"):
        try:
            return importlib.metadata.version(name)
        except importlib.metadata.PackageNotFoundError:
            pass
    return "unknown"


def as_text(value) -> str:
    if value is None:
        return ""
    if isinstance(value, str):
        return value
    return str(value)


def collect_reports(state: dict) -> dict:
    reports = {}
    keys = (
        "market_report", "sentiment_report", "news_report",
        "fundamentals_report", "policy_report", "hot_money_report",
        "lockup_report", "data_quality_summary", "investment_plan",
        "trader_investment_plan", "final_trade_decision",
    )
    for key in keys:
        text = as_text(state.get(key)).strip()
        if text:
            reports[key] = text

    debate = state.get("investment_debate_state") or {}
    for source, target in (
        ("bull_history", "bull_history"),
        ("bear_history", "bear_history"),
        ("judge_decision", "research_manager"),
    ):
        text = as_text(debate.get(source)).strip()
        if text:
            reports[target] = text

    risk = state.get("risk_debate_state") or {}
    for source, target in (
        ("aggressive_history", "aggressive_analyst"),
        ("conservative_history", "conservative_analyst"),
        ("neutral_history", "neutral_analyst"),
        ("judge_decision", "portfolio_manager"),
    ):
        text = as_text(risk.get(source)).strip()
        if text:
            reports[target] = text
    return reports


def main() -> int:
    args = parse_args()
    output = Path(args.output).expanduser().resolve()
    repo = Path(args.repo).expanduser().resolve()
    work_dir = Path(args.work_dir).expanduser().resolve()

    if sys.version_info < (3, 10):
        raise RuntimeError(
            "TradingAgents-Astock 需要 Python >= 3.10；当前为 "
            + sys.version.split()[0]
        )
    if not (repo / "tradingagents" / "graph" / "trading_graph.py").is_file():
        raise RuntimeError("不是有效的 tradingagents-astock 目录: %s" % repo)

    sys.path.insert(0, str(repo))
    os.chdir(str(repo))
    try:
        from dotenv import load_dotenv
        load_dotenv(repo / ".env", override=False)
    except ImportError:
        pass

    from tradingagents.default_config import DEFAULT_CONFIG
    from tradingagents.graph.trading_graph import TradingAgentsGraph

    config = copy.deepcopy(DEFAULT_CONFIG)
    if args.provider:
        config["llm_provider"] = args.provider
    if args.deep_model:
        config["deep_think_llm"] = args.deep_model
    if args.quick_model:
        config["quick_think_llm"] = args.quick_model
    if args.backend_url:
        config["backend_url"] = args.backend_url

    provider = str(config.get("llm_provider") or "")
    credential_env = CREDENTIALS.get(provider, "")
    engine = {
        "name": "TradingAgents-Astock",
        "version": package_version(),
        "repo_path": str(repo),
    }

    if args.check:
        write_json(output, {
            "schema_version": SCHEMA_VERSION,
            "status": "ok",
            "python_version": sys.version.split()[0],
            "engine": engine,
            "provider": provider,
            "credential_env": credential_env,
            "credential_set": not credential_env or bool(os.getenv(credential_env)),
        })
        return 0

    if not args.ticker or len(args.ticker) != 6 or not args.ticker.isdigit():
        raise RuntimeError("analyze 需要六位沪深股票代码")
    try:
        datetime.strptime(args.date or "", "%Y-%m-%d")
    except ValueError as exc:
        raise RuntimeError("分析日期必须为 YYYY-MM-DD") from exc

    config["data_vendors"] = {
        "core_stock_apis": "a_stock",
        "technical_indicators": "a_stock",
        "fundamental_data": "a_stock",
        "news_data": "a_stock",
        "signal_data": "a_stock",
    }
    config["output_language"] = "Chinese"
    config["checkpoint_enabled"] = bool(args.checkpoint)
    config["results_dir"] = str(work_dir / "logs")
    config["data_cache_dir"] = str(work_dir / "cache")
    config["memory_log_path"] = str(work_dir / "memory" / "trading_memory.md")
    for directory in (work_dir / "logs", work_dir / "cache", work_dir / "memory"):
        directory.mkdir(parents=True, exist_ok=True)

    print(
        "[analysis] 启动 %s，provider=%s，ticker=%s，date=%s"
        % (engine["name"], provider, args.ticker, args.date),
        file=sys.stderr,
        flush=True,
    )
    started = time.monotonic()
    graph = TradingAgentsGraph(debug=False, config=config)
    final_state, signal = graph.propagate(args.ticker, args.date)
    duration = time.monotonic() - started
    created_at = datetime.now(timezone.utc).astimezone().isoformat(timespec="seconds")
    identifier = "%s-%s" % (
        datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ"),
        uuid.uuid4().hex[:8],
    )
    result = {
        "schema_version": SCHEMA_VERSION,
        "status": "ok",
        "id": identifier,
        "ticker": args.ticker,
        "trade_date": args.date,
        "created_at": created_at,
        "duration_seconds": round(duration, 3),
        "signal": as_text(signal),
        "provider": provider,
        "deep_model": str(config.get("deep_think_llm") or ""),
        "quick_model": str(config.get("quick_think_llm") or ""),
        "engine": engine,
        "data_vendors": config["data_vendors"],
        "reports": collect_reports(final_state),
        "disclaimer": DISCLAIMER,
    }
    write_json(output, result)
    print(
        "[analysis] 完成，signal=%s，耗时 %.1fs" % (signal, duration),
        file=sys.stderr,
        flush=True,
    )
    return 0


if __name__ == "__main__":
    destination = None
    try:
        parsed = parse_args()
        destination = Path(parsed.output).expanduser().resolve()
        # Parse once here only to guarantee an error file can be emitted. main()
        # parses again to keep the normal entry point straightforward.
        raise SystemExit(main())
    except SystemExit:
        raise
    except Exception as exc:
        if destination is not None:
            try:
                write_json(destination, {
                    "schema_version": SCHEMA_VERSION,
                    "status": "error",
                    "error": str(exc),
                })
            except Exception:
                pass
        print("[analysis] 失败: %s" % exc, file=sys.stderr)
        if os.getenv("ASTOCK_ANALYSIS_DEBUG"):
            traceback.print_exc()
        raise SystemExit(2)
