import argparse
from pathlib import Path

from playwright.sync_api import sync_playwright, expect


def check_page(page, base_url, output):
    page.goto(base_url + "/?view=monitor&monitor=playbook", wait_until="networkidle")
    expect(page.get_by_role("tab", name="交易剧本", exact=True)).to_have_attribute("aria-selected", "true")
    expect(page.locator(".playbook-kpis")).to_contain_text("5 条已复盘")
    expect(page.locator(".playbook-kpis > div").nth(2).locator("strong")).to_have_text("3")
    expect(page.locator(".playbook-kpis > div").nth(3).locator("strong")).to_have_text("+0.60R / +0.80R")
    expect(page.locator(".playbook-metric-row").filter(has_text="区间突破")).to_contain_text("样本不足")
    expect(page.locator(".playbook-ledger tbody tr")).to_have_count(5)
    page.get_by_role("button", name="标签", exact=True).click()
    expect(page.locator(".playbook-metric-row").filter(has_text="放量")).to_be_visible()
    page.get_by_role("button", name="已完成", exact=True).click()
    expect(page.locator(".playbook-ledger tbody tr")).to_have_count(3)
    page.get_by_role("button", name="有偏差", exact=True).click()
    expect(page.locator(".playbook-ledger tbody tr")).to_have_count(1)
    expect(page.locator(".playbook-ledger tbody tr")).to_contain_text("平安银行")
    page.get_by_role("button", name="全部", exact=True).click()
    page.get_by_role("textbox", name="筛选交易剧本", exact=True).fill("贵州茅台")
    expect(page.locator(".playbook-ledger tbody tr")).to_have_count(2)
    page.get_by_role("textbox", name="筛选交易剧本", exact=True).fill("")
    assert page.evaluate("document.documentElement.scrollWidth <= window.innerWidth"), "page overflow"
    if page.viewport_size["width"] <= 720:
        overflow = page.locator(".trade-playbook :is(strong, small, button, td, span)").evaluate_all("""elements => elements.filter(el => el.getClientRects().length && el.clientWidth > 0 && el.scrollWidth > el.clientWidth + 2).map(el => el.textContent)""")
        assert not overflow, overflow
    zero_bars = page.locator(".playbook-r-bars > div[data-count='0'] i").evaluate_all("els => els.map(el => el.getBoundingClientRect().width)")
    assert zero_bars and all(width == 0 for width in zero_bars), zero_bars
    page.locator(".trade-playbook").screenshot(path=str(output))


def check_review_round_trip(page, base_url):
    rows = page.locator(".playbook-ledger tbody tr")
    row = rows.filter(has_text="贵州茅台").filter(has_text="趋势回踩")
    row.get_by_role("button", name="打开原计划", exact=True).click()
    plan_id = "c" * 64
    plan = page.locator(f".chart-saved-plan[data-plan-id='{plan_id}']")
    expect(plan).to_have_attribute("open", "")
    expect(plan.locator(".plan-review")).to_have_attribute("open", "")
    expect(plan.locator(":scope > summary")).to_contain_text("趋势回踩")
    note = "本次复盘在自动刷新后继续保留"
    plan.get_by_label("复盘备注", exact=True).fill(note)
    plan.get_by_label("实际退出价", exact=True).fill("104")
    # Wait for the normal ten-second quote refresh, including its review GET.
    with page.expect_response(lambda response: "/api/trade-plan-reviews?symbol=" in response.url and response.request.method == "GET", timeout=20000):
        pass
    expect(plan.get_by_label("复盘备注", exact=True)).to_have_value(note)
    expect(plan.get_by_label("实际退出价", exact=True)).to_have_value("104")
    with page.expect_response(lambda response: response.url.endswith("/api/trade-plan-reviews") and response.request.method == "POST") as saved:
        plan.get_by_role("button", name="保存复盘版本", exact=True).click()
    assert saved.value.status == 200
    revision = saved.value.json()
    assert revision["sequence"] == 2 and revision["revisions"][0]["actual_exit"] == 101.6
    expect(plan.locator(".plan-review-result")).to_contain_text("费用前 2.00R")
    plan.get_by_role("button", name="交易剧本", exact=True).click()
    expect(page.locator(".playbook-kpis > div").nth(3).locator("strong")).to_have_text("+1.00R / +2.00R")
    page.reload(wait_until="networkidle")
    expect(page.locator(".playbook-kpis > div").nth(3).locator("strong")).to_have_text("+1.00R / +2.00R")

    # Restoring the temporary fixture keeps viewport checks independent.
    result = page.request.post(base_url + "/api/trade-plan-reviews", data={"symbol": "sh600519", "plan_id": plan_id, "execution_status": "followed", "discipline": "partial", "actual_entry": 100, "actual_exit": 101.6, "tags": ["趋势"], "exit_reason": "按剧本复盘"})
    assert result.status == 200


def check_empty_and_errors(page, base_url):
    def failed_report(route):
        route.fulfill(status=500, json={"error": "读取计划复盘失败: 测试读取故障"})

    page.route("**/api/trade-playbook", failed_report)
    page.reload(wait_until="networkidle")
    expect(page.locator(".trade-playbook [role='alert']")).to_contain_text("测试读取故障")
    page.unroute("**/api/trade-playbook", failed_report)
    empty = {"report": {"total_plans": 0, "reviewed_plans": 0, "completed_trades": 0, "setups": [], "tags": [], "recent": [], "r_distribution": []}}
    page.route("**/api/trade-playbook", lambda route: route.fulfill(json=empty))
    page.reload(wait_until="networkidle")
    expect(page.locator(".playbook-kpis > div").nth(3).locator("strong")).to_have_text("-- / --")
    expect(page.locator(".playbook-ledger tbody tr")).to_have_count(0)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--url", default="http://127.0.0.1:18794")
    parser.add_argument("--screenshots", default="/private/tmp/astock-trade-playbook")
    args = parser.parse_args()
    output = Path(args.screenshots)
    output.mkdir(parents=True, exist_ok=True)
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=True)
        errors = []
        desktop = browser.new_page(viewport={"width": 1440, "height": 1100})
        desktop.on("pageerror", lambda error: errors.append(str(error)))
        check_page(desktop, args.url, output / "desktop.png")
        check_review_round_trip(desktop, args.url)
        desktop.close()
        mobile = browser.new_page(viewport={"width": 390, "height": 844}, is_mobile=True, has_touch=True)
        mobile.on("pageerror", lambda error: errors.append(str(error)))
        check_page(mobile, args.url, output / "mobile.png")
        mobile.close()
        narrow = browser.new_page(viewport={"width": 320, "height": 844}, is_mobile=True, has_touch=True)
        narrow.on("pageerror", lambda error: errors.append(str(error)))
        check_page(narrow, args.url, output / "mobile-320.png")
        check_empty_and_errors(narrow, args.url)
        narrow.close()
        assert not errors, errors
        browser.close()
    print("PASS: playbook statistics, filters, plan link, draft preservation, append-only review round trip, reload, empty/error states and 1440/390/320 layouts")


if __name__ == "__main__":
    main()
