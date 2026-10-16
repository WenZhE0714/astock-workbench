import argparse
import json
from pathlib import Path

from playwright.sync_api import expect, sync_playwright

parser = argparse.ArgumentParser()
parser.add_argument('--url', required=True)
parser.add_argument('--output', default='/private/tmp/astock-shadow-comparison-browser')
args = parser.parse_args()
output = Path(args.output)
output.mkdir(parents=True, exist_ok=True)
expect.set_options(timeout=30000)

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    try:
        page = browser.new_page(viewport={'width': 1440, 'height': 1000}, accept_downloads=True)
        errors = []
        page.on('pageerror', lambda error: errors.append(str(error)))
        page.goto(args.url + '/?view=realtime&realtime=shadow&shadow=comparison', wait_until='networkidle')
        panel = page.get_by_role('region', name='多日对照评估')
        expect(panel).to_be_visible()
        expect(panel.locator('[data-comparison-profile]')).to_have_count(6)
        expect(panel).to_contain_text('交易日日历已覆盖')
        page.get_by_role('button', name='查看均衡延伸上限曲线', exact=True).click()
        expect(panel.locator('[data-comparison-missing]')).to_contain_text('2026-10-13')
        page.get_by_role('button', name='查看均衡对照曲线', exact=True).click()
        expect(panel.get_by_role('img', name='均衡对照封存收益曲线')).to_be_visible()
        assert panel.locator('.sc-account').get_attribute('d')
        start = page.get_by_label('对照开始日期', exact=True)
        start.fill('2026-10-15')
        expect(panel).to_have_attribute('aria-busy', 'false')
        expect(panel.locator('[data-comparison-profile="trial-balanced"]')).to_contain_text('1 / 1')
        with page.expect_download() as download:
            page.get_by_role('link', name='导出多日对照', exact=True).click()
        file = output / 'comparison.json'
        download.value.save_as(file)
        report = json.loads(file.read_text())
        assert len(report['rows']) == 6
        assert all(row['max_drawdown_percent'] is None for row in report['rows'])
        page.get_by_role('button', name='打开对照账本', exact=True).click()
        expect(panel).to_have_count(0)
        page.get_by_role('button', name='多日对照', exact=True).click()
        panel = page.get_by_role('region', name='多日对照评估')
        expect(page.get_by_label('对照开始日期', exact=True)).to_have_value('2026-10-15')
        expect(panel).to_have_attribute('aria-busy', 'false')
        for width in [1440, 900, 390, 320]:
            page.set_viewport_size({'width': width, 'height': 1000 if width > 600 else 844})
            assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'), width
            clipped = panel.locator('b,strong,p,dd,span,small').evaluate_all('items => items.filter(el => el.getClientRects().length && el.clientWidth && el.scrollWidth > el.clientWidth + 2).map(el => el.textContent)')
            assert not clipped, clipped
            page.screenshot(path=str(output / f'comparison-{width}.png'), full_page=True)
        assert not errors, errors
        print({'missing_dates': True, 'single_day_drawdown_unavailable': True, 'date_preserved': True, 'export': True, 'viewports': [1440, 900, 390, 320]})
    finally:
        browser.close()
