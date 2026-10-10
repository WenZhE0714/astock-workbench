import argparse
from pathlib import Path

from playwright.sync_api import sync_playwright, expect


def check_layout(page):
    assert page.evaluate("document.documentElement.scrollWidth <= innerWidth"), "page overflows"
    overflowing = page.locator(".chart-analysis-band :is(select, input, strong, dd, p, button), .chart-structure-layers :is(label, span)").evaluate_all("""elements => elements.filter(el => el.getClientRects().length && el.clientWidth > 0 && el.scrollWidth > el.clientWidth + 2).map(el => el.textContent)""")
    assert not overflowing, overflowing


def wait_for_draw(page):
    page.evaluate("() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))")


def capture_canvas(page):
    wait_for_draw(page)
    page.locator("canvas[aria-label='日K线图']").evaluate("canvas => { window.patternBaseline = canvas.getContext('2d').getImageData(0,0,canvas.width,canvas.height).data; }")


def changed_pixels(page):
    wait_for_draw(page)
    return page.locator("canvas[aria-label='日K线图']").evaluate("""canvas => {
      const pixels = canvas.getContext('2d').getImageData(0,0,canvas.width,canvas.height).data;
      const before = window.patternBaseline;
      let changed = 0;
      for(let i=0;i<pixels.length;i+=4) if(pixels[i] !== before[i] || pixels[i+1] !== before[i+1] || pixels[i+2] !== before[i+2]) changed++;
      return changed;
    }""")


def verify_overlay(page):
    toggle = page.get_by_label("结构标注", exact=True)
    toggle.uncheck()
    capture_canvas(page)
    toggle.check()
    changed = changed_pixels(page)
    assert changed > 180, f"structure toggle did not draw geometry: {changed} pixels"


def layer_checkbox(page, pattern):
    return page.locator(f".chart-layer-item[data-structure-id='{pattern}'] input")


def open_symbol(page, symbol):
    page.get_by_role("textbox", name="股票、转债、板块代码或名称", exact=True).fill(symbol)
    with page.expect_response(lambda response: "/api/stock?" in response.url and symbol in response.url):
        page.get_by_role("button", name="查看行情", exact=True).click()


def verify_concurrent_layers(page, width, output):
    open_symbol(page, "sh600003")
    select = page.get_by_role("combobox", name="观察形态", exact=True)
    expect(select).to_have_value("double-bottom")
    bottom = layer_checkbox(page, "double-bottom")
    triangle = layer_checkbox(page, "ascending-triangle")
    expect(bottom).to_be_checked()
    expect(triangle).to_be_checked()
    plan_values = page.locator(".chart-plan-values").inner_text()
    capture_canvas(page)
    bottom.uncheck()
    assert changed_pixels(page) > 100, "double-bottom layer did not change the canvas"
    expect(triangle).to_be_checked()
    expect(select).to_have_value("double-bottom")
    assert page.locator(".chart-plan-values").inner_text() == plan_values
    with page.expect_response(lambda response: "/api/chart-analysis?" in response.url):
        page.get_by_role("button", name="刷新结构分析", exact=True).click()
    expect(bottom).not_to_be_checked()
    expect(triangle).to_be_checked()
    if width == 1440:
        page.wait_for_event("response", predicate=lambda response: "/api/stock?" in response.url and "sh600003" in response.url, timeout=15000)
        expect(bottom).not_to_be_checked()
        expect(triangle).to_be_checked()
    select.select_option("ascending-triangle")
    expect(bottom).not_to_be_checked()
    expect(triangle).to_be_checked()
    page.get_by_role("tab", name="分时", exact=True).click()
    expect(page.get_by_role("group", name="形态图层", exact=True)).to_have_count(0)
    page.get_by_role("tab", name="日 K", exact=True).click()
    expect(bottom).not_to_be_checked()
    expect(triangle).to_be_checked()
    bottom.check()
    capture_canvas(page)
    triangle.uncheck()
    assert changed_pixels(page) > 100, "triangle layer did not change the canvas"
    expect(bottom).to_be_checked()
    expect(select).to_have_value("ascending-triangle")
    all_layers = page.get_by_label("全部形态", exact=True)
    assert all_layers.evaluate("input => input.indeterminate")
    all_layers.check()
    assert not all_layers.evaluate("input => input.indeterminate")
    expect(page.locator(".chart-layer-item input:not(:checked)")).to_have_count(0)
    all_layers.uncheck()
    expect(page.locator(".chart-layer-item input:checked")).to_have_count(0)
    capture_canvas(page)
    toggle = page.get_by_label("结构标注", exact=True)
    toggle.uncheck()
    assert changed_pixels(page) == 0, "empty selection still drew structure annotations"
    expect(bottom).to_be_disabled()
    toggle.check()
    expect(page.locator(".chart-layer-item input:checked")).to_have_count(0)
    all_layers.check()
    verify_overlay(page)
    check_layout(page)
    page.locator(".chart-panel").screenshot(path=str(output / f"concurrent-patterns-{width}.png"))
    bottom.uncheck()
    open_symbol(page, "sh600519")
    expect(layer_checkbox(page, "double-bottom")).to_be_checked()
    expect(layer_checkbox(page, "ascending-triangle")).to_have_count(0)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--url", default="http://127.0.0.1:18796")
    parser.add_argument("--screenshots", default="/private/tmp/astock-chart-patterns")
    args = parser.parse_args()
    output = Path(args.screenshots)
    output.mkdir(parents=True, exist_ok=True)
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=True)
        errors = []
        for width in [1440, 390, 320]:
            page = browser.new_page(viewport={"width": width, "height": 1100 if width > 600 else 844}, is_mobile=width < 600, has_touch=width < 600)
            page.on("pageerror", lambda error: errors.append(str(error)))
            page.goto(args.url + "/?view=market&chart=daily", wait_until="networkidle")
            for symbol, pattern, bias in [("sh600519", "double-bottom", "看涨结构"), ("sh600000", "double-top", "看跌结构"), ("sh600001", "ascending-triangle", "看涨结构"), ("sh600002", "descending-triangle", "看跌结构"), ("sh600004", "head-shoulders-bottom", "看涨结构"), ("sh600005", "head-shoulders-top", "看跌结构"), ("sh600006", "bull-flag", "看涨结构"), ("sh600007", "bear-flag", "看跌结构")]:
                if symbol != "sh600519":
                    open_symbol(page, symbol)
                select = page.get_by_role("combobox", name="观察形态", exact=True)
                expect(select.locator(f"option[value='{pattern}']")).to_have_count(1)
                expect(select).to_have_value(pattern)
                expect(layer_checkbox(page, pattern)).to_be_checked()
                layers_box = page.get_by_role("group", name="形态图层", exact=True).bounding_box()
                canvas_box = page.locator("canvas[aria-label='日K线图']").bounding_box()
                assert layers_box["y"] + layers_box["height"] <= canvas_box["y"] + 1, "layer controls are below the chart"
                select.select_option(pattern)
                expect(page.locator(".chart-pattern-facts")).to_contain_text(bias)
                expect(page.locator(".chart-structure-state")).to_have_text("日线已确认")
                expect(page.locator(".chart-pattern-facts")).to_contain_text("确认日 2026-09-18")
                if pattern.startswith("head-shoulders"):
                    expect(page.locator(".chart-structure-evidence")).to_contain_text("近水平颈线")
                    for label in ["左肩", "头部", "右肩", "左颈点", "右颈点"]:
                        expect(page.locator(".chart-anchor-dates")).to_contain_text(label)
                if pattern.endswith("flag"):
                    expect(page.locator(".chart-structure-evidence")).to_contain_text("逆向近乎平行")
                    expect(page.locator(".chart-structure-evidence")).to_contain_text("不随之后的通道投影移动")
                    for label in ["旗杆起点", "旗杆终点", "旗低3" if pattern == "bull-flag" else "旗高3"]:
                        expect(page.locator(".chart-anchor-dates")).to_contain_text(label)
                verify_overlay(page)
                check_layout(page)
                page.locator(".chart-panel").screenshot(path=str(output / f"{pattern}-{width}.png"))
                if bias == "看跌结构":
                    expect(page.get_by_role("button", name="保存观察计划", exact=True)).to_have_count(0)
                    expect(page.get_by_label("计划价位", exact=True)).to_be_disabled()
                    continue
                if width == 1440:
                    page.get_by_label("计划有效期", exact=True).fill("2026-09-25")
                    with page.expect_response(lambda response: response.url.endswith("/api/trade-plans") and response.request.method == "POST") as saved:
                        page.get_by_role("button", name="保存观察计划", exact=True).click()
                    assert saved.value.status in [200, 201]
                    plan = saved.value.json()["plan"]
                    assert plan["structure"]["id"] == pattern, "visible overlays changed the saved plan target"
                    assert plan["monitor_rule"]["pattern_ready_on"] == plan["structure"]["pattern"]["ready_on"]
                    details = page.locator(f".chart-saved-plan[data-plan-id='{plan['id']}']")
                    details.locator(":scope > summary").click()
                    with page.expect_response(lambda response: response.url.endswith("/api/trade-plan-monitors") and response.request.method == "POST") as enabled:
                        details.locator(".plan-monitor-switch input").check()
                    assert enabled.value.status == 200
                    assert enabled.value.json()["rule"]["breakout_price"] == plan["structure"]["pattern"]["trigger_price"]
                    loaded = page.request.get(args.url + f"/api/trade-plans?symbol={symbol}&plan_id={plan['id']}").json()["items"]
                    assert next(item for item in loaded if item["id"] == plan["id"])["structure"] == plan["structure"]
            verify_concurrent_layers(page, width, output)
            if width == 1440:
                for symbol, pattern in [("sh600519", "double-bottom"), ("sh600004", "head-shoulders-bottom"), ("sh600005", "head-shoulders-top"), ("sh600006", "bull-flag"), ("sh600007", "bear-flag")]:
                    open_symbol(page, symbol)
                    page.get_by_role("combobox", name="观察形态", exact=True).select_option(pattern)
                    page.get_by_role("tab", name="1月", exact=True).click()
                    canvas = page.locator("canvas[aria-label='日K线图']")
                    canvas.scroll_into_view_if_needed()
                    box = canvas.bounding_box()
                    page.mouse.move(box["x"] + box["width"] * .15, box["y"] + box["height"] * .45)
                    page.mouse.down()
                    page.mouse.move(box["x"] + box["width"] * .95, box["y"] + box["height"] * .45, steps=10)
                    page.mouse.up()
                    expect(page.locator(".chart-analysis-date")).not_to_contain_text("2026-09-18")
                    expect(page.get_by_role("combobox", name="观察形态", exact=True)).not_to_have_value(pattern)
                    if not pattern.endswith("flag"):
                        expect(page.get_by_role("combobox", name="观察形态", exact=True)).to_have_value("range-breakout")
                        expect(page.locator(".chart-pattern-facts")).to_have_count(0)
                    expect(layer_checkbox(page, pattern)).to_have_count(0)
                    check_layout(page)
                    canvas.dblclick()
                    expect(page.locator(".chart-analysis-date")).to_contain_text("2026-09-18")
                    page.get_by_role("combobox", name="观察形态", exact=True).select_option(pattern)
                    expect(page.locator(".chart-structure-state")).to_have_text("日线已确认")
            page.close()
        assert not errors, errors
        browser.close()
    print("PASS: eight classic patterns, concurrent pattern pixels, independent layer/plan selections, select-all, live refresh, stock reset, historical cutoff, frozen plans and 1440/390/320 layouts")


if __name__ == "__main__":
    main()
