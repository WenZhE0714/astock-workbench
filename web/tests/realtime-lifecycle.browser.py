import argparse
from pathlib import Path

from playwright.sync_api import sync_playwright, expect


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--url", default="http://127.0.0.1:18793")
    parser.add_argument("--screenshots", default="/private/tmp/astock-realtime-lifecycle")
    args = parser.parse_args()
    output = Path(args.screenshots)
    output.mkdir(parents=True, exist_ok=True)

    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=True)
        errors = []
        for width, height, name in [(1440, 1000, "desktop"), (390, 844, "mobile")]:
            page = browser.new_page(viewport={"width": width, "height": height}, is_mobile=width < 600, has_touch=width < 600)
            page.on("pageerror", lambda error: errors.append(str(error)))
            page.goto(args.url + "/?view=realtime&realtime=lifecycle", wait_until="networkidle")
            expect(page.get_by_role("tab", name="信号轨迹", exact=False)).to_have_attribute("aria-selected", "true")
            expect(page.locator(".signal-lifecycle-panel")).to_be_visible()
            expect(page.locator(".lifecycle-list button")).to_have_count(2)
            expect(page.locator(".lifecycle-detail")).to_contain_text("贵州茅台")
            expect(page.locator(".lifecycle-detail")).to_contain_text("1日 +2.50%")
            expect(page.locator(".lifecycle-event")).to_have_count(2)
            assert page.evaluate("document.documentElement.scrollWidth <= window.innerWidth"), "page overflow"
            page.locator(".signal-lifecycle-panel").screenshot(path=str(output / f"{name}.png"))
            page.close()
        assert not errors, errors
        browser.close()
    print("PASS: signal lifecycle desktop/mobile timeline, outcomes and overflow")


if __name__ == "__main__":
    main()
