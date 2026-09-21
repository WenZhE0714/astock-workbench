import argparse
from pathlib import Path

from playwright.sync_api import sync_playwright, expect


def open_stock(page, mobile=False):
    if mobile:
        page.get_by_role("combobox", name="切换工作区").select_option("market")
    else:
        page.get_by_role("button", name="个股行情", exact=True).click()
    page.get_by_role("tab", name="日 K", exact=True).click()
    expect(page.get_by_role("heading", name="结构观察", exact=True)).to_be_visible()
    expect(page.locator(".chart-analysis-date")).to_contain_text("2026-09-18")


def assert_layout(page):
    issues = page.locator(".chart-analysis-band").evaluate("""root => {
      const issues = [];
      for (const element of root.querySelectorAll('button, select, input, strong, dd, p')) {
        if (!element.getClientRects().length) continue;
        if (element.scrollWidth > element.clientWidth + 2) issues.push(element.textContent.slice(0, 60));
      }
      return issues;
    }""")
    assert not issues, f"Overflowing chart controls: {issues}"
    assert page.evaluate("document.documentElement.scrollWidth <= window.innerWidth"), "Page overflows horizontally"


def assert_canvas(page):
    result = page.locator("canvas[aria-label='日K线图']").evaluate("""canvas => {
      const pixels = canvas.getContext('2d').getImageData(0, 0, canvas.width, canvas.height).data;
      let colored = 0;
      for (let i = 0; i < pixels.length; i += 4) {
        if (pixels[i + 3] > 0 && Math.max(pixels[i], pixels[i+1], pixels[i+2]) - Math.min(pixels[i], pixels[i+1], pixels[i+2]) > 45) colored++;
      }
      return {colored, width:canvas.width, height:canvas.height};
    }""")
    assert result["colored"] > 200, f"Chart is blank: {result}"


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--url", default="http://127.0.0.1:18791")
    parser.add_argument("--screenshots", default="/private/tmp/astock-chart-analysis")
    args = parser.parse_args()
    output = Path(args.screenshots)
    output.mkdir(parents=True, exist_ok=True)
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=True)
        errors = []
        page = browser.new_page(viewport={"width": 1440, "height": 1100})
        page.on("pageerror", lambda error: errors.append(str(error)))
        page.goto(args.url, wait_until="networkidle")
        open_stock(page)
        expect(page.locator(".chart-key-levels")).to_contain_text("MA20")
        expect(page.locator(".chart-key-levels")).not_to_contain_text("成交密集区")
        page.get_by_label("计划价位", exact=True).check()
        page.get_by_label("计划有效期", exact=True).fill("2026-09-25")
        page.get_by_role("button", name="保存观察计划", exact=True).click()
        expect(page.locator(".chart-plan-notice")).to_have_text("观察计划已保存")
        expect(page.locator(".chart-saved-plan")).to_have_count(1)
        page.locator(".chart-saved-plan > summary").click()
        expect(page.locator(".chart-saved-plan-body")).to_contain_text("2026-09-25")
        page.get_by_role("button", name="保存观察计划", exact=True).click()
        expect(page.locator(".chart-plan-notice")).to_contain_text("未重复创建")
        expect(page.locator(".chart-saved-plan")).to_have_count(1)
        monitor_toggle = page.locator(".chart-saved-plan .plan-monitor-switch input")
        expect(monitor_toggle).to_be_visible()
        with page.expect_response(lambda response: response.url.endswith("/api/trade-plan-monitors") and response.request.method == "POST") as enabled:
            monitor_toggle.check()
        assert enabled.value.status == 200
        expect(page.locator(".chart-saved-plan > summary")).to_contain_text("等待日线确认")
        expect(page.locator(".plan-monitor-events")).to_contain_text("启用监控")
        frozen_plan = page.locator(".chart-saved-plan-body > dl").inner_text()
        assert_layout(page)
        assert_canvas(page)
        page.locator(".workspace").screenshot(path=str(output / "desktop.png"))

        page.reload(wait_until="networkidle")
        open_stock(page)
        page.locator(".chart-saved-plans > summary").click()
        expect(page.locator(".chart-saved-plan")).to_have_count(1)
        page.locator(".chart-saved-plan > summary").click()
        monitor_toggle = page.locator(".chart-saved-plan .plan-monitor-switch input")
        expect(monitor_toggle).to_be_checked()
        with page.expect_response(lambda response: response.url.endswith("/api/trade-plan-monitors") and response.request.method == "POST") as paused:
            monitor_toggle.uncheck()
        assert paused.value.status == 200
        expect(page.locator(".chart-saved-plan > summary")).to_contain_text("已暂停")
        assert page.locator(".chart-saved-plan-body > dl").inner_text() == frozen_plan
        page.get_by_role("button", name="打开计划监控中心", exact=True).click()
        expect(page.locator(".plan-monitor-record")).to_have_count(1)
        center_toggle = page.locator(".plan-monitor-record .plan-monitor-switch input")
        with page.expect_response(lambda response: response.url.endswith("/api/trade-plan-monitors") and response.request.method == "POST") as resumed:
            center_toggle.check()
        assert resumed.value.status == 200
        page.locator(".plan-monitor-center-details > summary").click()
        expect(page.locator(".plan-monitor-events")).to_contain_text("恢复监控")
        with page.expect_download() as download:
            page.get_by_role("link", name="导出记录", exact=True).click()
        assert download.value.failure() is None
        assert download.value.suggested_filename.startswith("plan-monitor-")
        page.locator(".monitor-view").screenshot(path=str(output / "monitor-center.png"))
        page.locator(".plan-monitor-stock").click()
        expect(page.get_by_role("heading", name="结构观察", exact=True)).to_be_visible()
        page.get_by_role("tab", name="1月", exact=True).click()
        canvas = page.locator("canvas[aria-label='日K线图']")
        canvas.scroll_into_view_if_needed()
        box = canvas.bounding_box()
        page.mouse.move(box["x"] + box["width"] * 0.4, box["y"] + box["height"] * 0.4)
        page.mouse.down()
        page.mouse.move(box["x"] + box["width"] * 0.8, box["y"] + box["height"] * 0.4, steps=10)
        page.mouse.up()
        expect(page.locator(".chart-analysis-date")).not_to_contain_text("2026-09-18")
        historical_date = page.locator(".chart-analysis-date").inner_text().split(" · ")[0]
        expect(page.locator(".chart-key-levels h2")).to_contain_text(historical_date)
        assert_layout(page)
        page.locator(".workspace").screenshot(path=str(output / "historical.png"))

        for width in [390, 320]:
            mobile = browser.new_page(viewport={"width": width, "height": 844}, is_mobile=True, has_touch=True)
            mobile.on("pageerror", lambda error: errors.append(str(error)))
            mobile.goto(args.url, wait_until="networkidle")
            open_stock(mobile, mobile=True)
            expect(mobile.locator(".assistant-launcher")).to_be_hidden()
            mobile.get_by_role("button", name="打开 AI 研究助手", exact=True).click()
            expect(mobile.get_by_role("dialog", name="AI 研究助手")).to_be_visible()
            mobile.get_by_role("button", name="关闭助手", exact=True).click()
            expect(mobile.get_by_role("dialog", name="AI 研究助手")).to_have_count(0)
            mobile.locator(".chart-saved-plans > summary").click()
            mobile.locator(".chart-saved-plan > summary").click()
            expect(mobile.locator(".plan-monitor-switch input")).to_be_checked()
            mobile.locator(".chart-analysis-band").scroll_into_view_if_needed()
            assert_layout(mobile)
            assert_canvas(mobile)
            mobile.locator(".chart-analysis-band").screenshot(path=str(output / f"mobile-{width}.png"))
            mobile.get_by_role("button", name="打开计划监控中心", exact=True).click()
            mobile.locator(".plan-monitor-center-details > summary").click()
            expect(mobile.locator(".plan-monitor-events")).to_contain_text("恢复监控")
            assert mobile.evaluate("document.documentElement.scrollWidth <= window.innerWidth"), "Mobile monitor center overflows"
            mobile.locator(".monitor-view").screenshot(path=str(output / f"mobile-monitor-{width}.png"))
            mobile.close()
        assert not errors, f"Browser errors: {errors}"
        browser.close()
    print("PASS: desktop/mobile layout, canvas, plan persistence, monitoring enable/pause/resume/reload, export, historical date isolation")


if __name__ == "__main__":
    main()
