import argparse
import copy
from pathlib import Path

from playwright.sync_api import sync_playwright, expect


def assert_layout(page):
    assert page.evaluate("document.documentElement.scrollWidth <= window.innerWidth"), "Page overflows"
    overflow = page.locator(".experiment-toolbar, .experiment-comparison, .experiment-plan-list").evaluate_all("""roots => roots.flatMap(root => [...root.querySelectorAll('button, label, strong, small')].filter(item => item.getClientRects().length && item.scrollWidth > item.clientWidth + 2).map(item => item.textContent))""")
    assert not overflow, overflow


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--url", default="http://127.0.0.1:18792")
    parser.add_argument("--screenshots", default="/private/tmp/astock-plan-experiment")
    args = parser.parse_args()
    output = Path(args.screenshots)
    output.mkdir(parents=True, exist_ok=True)
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=True)
        page = browser.new_page(viewport={"width": 1440, "height": 1000})
        errors = []
        page.on("pageerror", lambda error: errors.append(str(error)))
        page.goto(args.url + "/?view=market&chart=daily", wait_until="networkidle")
        page.get_by_role("button", name="保存观察计划", exact=True).click()
        page.locator(".chart-saved-plan > summary").first.click()
        with page.expect_response(lambda response: response.url.endswith("/api/trade-plan-monitors") and response.request.method == "POST") as monitoring:
            page.locator(".chart-saved-plan .plan-monitor-switch input").first.check()
        assert monitoring.value.status == 200
        page.get_by_role("button", name="打开计划监控中心", exact=True).first.click()
        page.get_by_role("tab", name="影子实验", exact=True).click()
        expect(page.locator(".plan-experiment")).to_be_visible()
        with page.expect_response(lambda response: response.url.endswith("/api/plan-experiment") and response.request.method == "POST") as selected:
            page.locator(".experiment-plan-row input").first.check()
        assert selected.value.status == 200
        expect(page.locator(".experiment-summary tbody tr")).to_have_count(2)
        with page.expect_response(lambda response: response.url.endswith("/api/plan-experiment") and response.request.method == "POST") as enabled:
            page.get_by_role("checkbox", name="允许实验新开仓", exact=True).check()
        assert enabled.value.status == 200
        page.goto(args.url + "/?view=monitor&monitor=experiment", wait_until="networkidle")
        expect(page.get_by_role("checkbox", name="允许实验新开仓", exact=True)).to_be_checked()
        expect(page.locator(".experiment-plan-row input").first).to_be_checked()
        with page.expect_response(lambda response: response.url.endswith("/api/plan-experiment") and response.request.method == "POST") as disabled:
            page.get_by_role("checkbox", name="允许实验新开仓", exact=True).uncheck()
        assert disabled.value.status == 200
        with page.expect_download() as exported:
            page.get_by_role("link", name="导出实验账本", exact=True).click()
        assert exported.value.failure() is None
        state = page.request.get(args.url + "/api/plan-experiment").json()
        assert not state["state"]["entries_enabled"]
        assert len(state["state"]["arms"]) == 2
        assert_layout(page)
        page.locator(".plan-experiment").screenshot(path=str(output / "controls.png"))

        # Nonzero display fixtures exercise the API contract; matching and
        # ledger invariants are covered by the Go engine/integration tests.
        fixture = copy.deepcopy(state)
        experiment = fixture["state"]
        plan_id = next(iter(experiment["selections"]))
        experiment.update(started_at="2026-09-21T09:30:00+08:00", valued_at="2026-09-22T10:00:00+08:00", valuation_complete=True)
        for arm, price, quantity in zip(experiment["arms"], [101, 103], [1000, 900]):
            amount = price * quantity
            fee = amount * .00031
            cash = 1_000_000 - amount - fee
            market = 107 * quantity
            profit = market - amount - fee
            order_id = arm["id"] + "-buy"
            arm["report"].update(total_equity=cash + market, remaining_cash=cash, total_market_value=market, total_profit=profit, total_return_percent=profit / 10_000, total_fees=fee, open_positions=1)
            arm["trials"][plan_id].update(status="open", reason="已模拟开仓", entry_order_id=order_id)
            arm["report"]["positions"] = [dict(signal_id=plan_id, symbol="sh600519", quantity=quantity, available_quantity=quantity, entry_price=price, last_price=107, unrealized_profit=profit, unrealized_return_percent=profit / (amount + fee) * 100, entry_time="2026-09-21 09:30:31", invalidation_price=95)]
            arm["report"]["orders"] = [dict(id=order_id, plan_id=plan_id, symbol="sh600519", execution_time="2026-09-21 09:30:31", side="buy", quantity=quantity, price=price, status="filled", reason="日线确认后的下一报价模拟成交")]
        experiment["equity"] = [dict(at=f"2026-09-22T09:{minute:02d}:00+08:00", range_equity=1_000_000 + index * 500, confirmed_equity=1_000_000 + index * 300) for index, minute in enumerate(range(30, 42))]

        def fixture_route(route):
            route.fulfill(json=fixture)

        page.route("**/api/plan-experiment", fixture_route)
        page.reload(wait_until="networkidle")
        expect(page.locator(".experiment-detail-table tbody tr")).to_have_count(1)
        assert_layout(page)
        pixels = page.locator("canvas[aria-label='双组收益曲线']").evaluate("""canvas => {
          const data = canvas.getContext('2d').getImageData(0,0,canvas.width,canvas.height).data;
          let count=0; for(let i=0;i<data.length;i+=4) if(data[i+3] && Math.max(data[i],data[i+1],data[i+2])-Math.min(data[i],data[i+1],data[i+2])>50) count++;
          return count;
        }""")
        assert pixels > 150, "Equity chart is blank"
        page.locator(".plan-experiment").screenshot(path=str(output / "desktop.png"))
        page.get_by_role("tab", name="仅确认对照组", exact=True).click()
        expect(page.locator(".experiment-detail-table")).to_contain_text("900 / 900")
        page.get_by_role("button", name="操作记录", exact=True).click()
        expect(page.locator(".experiment-detail-table")).to_contain_text("模拟买入")
        for width in [390, 320]:
            mobile = browser.new_page(viewport={"width": width, "height": 844}, is_mobile=True, has_touch=True)
            mobile.on("pageerror", lambda error: errors.append(str(error)))
            mobile.route("**/api/plan-experiment", fixture_route)
            mobile.goto(args.url + "/?view=monitor&monitor=experiment", wait_until="networkidle")
            assert_layout(mobile)
            mobile.locator(".plan-experiment").screenshot(path=str(output / f"mobile-{width}.png"))
            mobile.close()
        assert not errors, errors
        browser.close()
    print("PASS: experiment controls/reload/export, paired metrics, account switch, orders and responsive chart")


if __name__ == "__main__":
    main()
