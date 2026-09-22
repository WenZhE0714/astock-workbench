import argparse
import json
from pathlib import Path

from playwright.sync_api import sync_playwright, expect


def check_layout(page):
    assert page.evaluate("document.documentElement.scrollWidth <= innerWidth"), "Page overflows"
    overflowing = page.locator(".experiment-review :is(strong, small, dd, dt, button, input, select, p)").evaluate_all("""elements => elements.filter(el => el.getClientRects().length && el.clientWidth > 0 && el.scrollWidth > el.clientWidth + 2).map(el => el.textContent)""")
    assert not overflowing, overflowing


def check_review(page, base_url, output):
    page.goto(base_url + "/?view=monitor&monitor=experiment&experiment=review", wait_until="networkidle")
    expect(page.get_by_role("tab", name="自动复盘", exact=True)).to_have_attribute("aria-selected", "true")
    expect(page.locator(".experiment-review-kpis > div").first.locator("strong")).to_have_text("1 / 4")
    expect(page.locator(".experiment-review-plan")).to_have_count(4)
    baseline = page.request.get(base_url + "/api/plan-experiment?view=review").json()["review"]
    assert baseline["paired_completed"] == 1 and baseline["single_sided"] == 2
    select = page.get_by_label("自动复盘状态", exact=True)
    select.select_option("single_sided")
    expect(page.locator(".experiment-review-plan")).to_have_count(2)
    for result in baseline["plans"]:
        if result["status"] == "single_sided":
            assert "return_difference" not in result and "net_r_difference" not in result
    page.get_by_role("textbox", name="筛选自动复盘", exact=True).fill("银行")
    expect(page.locator(".experiment-review-plan")).to_have_count(1)
    expect(page.locator(".experiment-review-plan")).to_contain_text("平安银行")
    page.get_by_role("textbox", name="筛选自动复盘", exact=True).fill("")
    select.select_option("waiting")
    expect(page.locator(".experiment-review-plan")).to_have_count(1)
    page.locator(".experiment-review-plan > summary").click()
    expect(page.locator(".experiment-review-rejection").first).to_contain_text("真实成交额")
    select.select_option("paired_closed")
    plan_id = "a" * 64
    record = page.locator(f".experiment-review-plan[data-plan-id='{plan_id}']")
    expect(record).to_contain_text("已移出实验")
    record.locator(":scope > summary").click()
    expect(record).to_contain_text("09/22 10:00:00")
    expect(record).to_contain_text("跌破失效位")
    pair = next(row for row in baseline["plans"] if row["plan_id"] == plan_id)
    expected_r = f"{pair['range']['net_r']:.2f}R"
    expect(record.locator("summary")).to_contain_text(expected_r)
    check_layout(page)
    page.locator(".experiment-review").screenshot(path=str(output))

    with page.expect_download() as event:
        page.get_by_role("link", name="导出自动复盘", exact=True).click()
    download = event.value
    assert download.failure() is None
    exported = json.loads(Path(download.path()).read_text())
    assert exported["review"] == baseline

    page.get_by_role("tab", name="账户", exact=True).click()
    expect(page.get_by_role("checkbox", name="允许实验新开仓", exact=True)).not_to_be_checked()
    canvas = page.locator("canvas[aria-label='双组收益曲线']")
    expect(canvas).to_be_visible()
    page.wait_for_function("""() => {
      const canvas = document.querySelector("canvas[aria-label='双组收益曲线']");
      if (!canvas || !canvas.width || !canvas.height) return false;
      const data = canvas.getContext('2d').getImageData(0,0,canvas.width,canvas.height).data;
      let colored=0;
      for(let i=0;i<data.length;i+=4) if(data[i+3] && Math.max(data[i],data[i+1],data[i+2])-Math.min(data[i],data[i+1],data[i+2])>50) colored++;
      return colored > 80;
    }""")
    page.get_by_role("tab", name="自动复盘", exact=True).click()
    select = page.get_by_label("自动复盘状态", exact=True)
    select.select_option("paired_closed")
    record = page.locator(f".experiment-review-plan[data-plan-id='{plan_id}']")
    record.locator(":scope > summary").click()
    record.get_by_role("button", name="查看原计划 aaaaaaaa", exact=True).click()
    expect(page.locator(f".chart-saved-plan[data-plan-id='{plan_id}']")).to_have_attribute("open", "")
    page.goto(base_url + "/?view=monitor&monitor=experiment&experiment=review", wait_until="networkidle")
    after = page.request.get(base_url + "/api/plan-experiment?view=review").json()["review"]
    assert after == baseline
    expect(page.locator(".experiment-review-kpis > div").first.locator("strong")).to_have_text("1 / 4")


def check_empty_and_error(page, base_url):
    response = page.request.get(base_url + "/api/plan-experiment").json()
    empty = {"version": "plan-pair-review-v1", "initialized": False, "total_plans": 0, "paired_completed": 0, "single_sided": 0, "open_pairs": 0, "waiting": 0, "unavailable": 0, "range_higher": 0, "confirmed_higher": 0, "equal_return": 0, "net_r_samples": 0, "arms": [], "plans": []}

    def empty_route(route):
        route.fulfill(json={**response, "review": empty})

    page.route("**/api/plan-experiment", empty_route)
    page.reload(wait_until="networkidle")
    expect(page.locator(".experiment-review-kpis > div").nth(1).locator("strong")).to_have_text("--")
    expect(page.locator(".experiment-review")).to_contain_text("尚无实验计划")
    page.unroute("**/api/plan-experiment", empty_route)
    page.route("**/api/plan-experiment", lambda route: route.fulfill(status=500, json={"error": "测试账本读取失败"}))
    page.reload(wait_until="networkidle")
    expect(page.locator(".plan-experiment [role='alert']")).to_contain_text("测试账本读取失败")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--url", default="http://127.0.0.1:18795")
    parser.add_argument("--screenshots", default="/private/tmp/astock-experiment-review")
    args = parser.parse_args()
    output = Path(args.screenshots)
    output.mkdir(parents=True, exist_ok=True)
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=True)
        errors = []
        for width in [1440, 390, 320]:
            page = browser.new_page(viewport={"width": width, "height": 1000 if width > 600 else 844}, is_mobile=width < 600, has_touch=width < 600)
            page.on("pageerror", lambda error: errors.append(str(error)))
            check_review(page, args.url, output / f"review-{width}.png")
            if width == 320:
                check_empty_and_error(page, args.url)
            page.close()
        assert not errors, errors
        browser.close()
    print("PASS: engine-derived automatic review, paired/unpaired filters, ledger export, account canvas, original plan, reload, empty/error states and 1440/390/320 layouts")


if __name__ == "__main__":
    main()
