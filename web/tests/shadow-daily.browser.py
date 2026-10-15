import argparse
import json
from datetime import date, timedelta
from pathlib import Path

from playwright.sync_api import expect, sync_playwright

parser = argparse.ArgumentParser()
parser.add_argument('--url', required=True)
parser.add_argument('--output', default='/private/tmp/astock-shadow-daily-browser')
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
        page.goto(args.url + '/?view=realtime&realtime=shadow&shadow=daily', wait_until='networkidle')
        panel = page.get_by_role('region', name='每日对照复盘')
        expect(panel).to_be_visible()
        today = page.request.get(args.url + '/api/strategy/shadow-daily').json()['today']
        field = page.get_by_label('影子复盘日期', exact=True)
        expect(field).to_have_value(today)
        page.get_by_role('button', name='优化对照', exact=True).click()
        expect(panel.locator('[data-daily-profile]')).to_have_count(6)
        page.get_by_role('button', name='查看抓妖对照每日明细', exact=True).click()
        expect(panel.locator('[data-gate-code="monster-bear"]')).to_be_visible()
        expect(panel.locator('[data-zero-trade-reason]')).to_contain_text('熊市')
        missing = (date.fromisoformat(today) - timedelta(days=2)).isoformat()
        field.fill(missing)
        expect(panel.locator('.sd-warnings')).to_contain_text('没有扫描档案')
        expect(panel.locator('[data-zero-trade-reason]')).to_contain_text('未留存扫描档案')
        yesterday = (date.fromisoformat(today) - timedelta(days=1)).isoformat()
        field.fill(yesterday)
        expect(panel.locator('.sd-facts')).to_contain_text('当日权益快照未留存')
        expect(panel.locator('[data-zero-trade-reason]')).to_contain_text('起点之后')
        page.get_by_role('button', name='打开当前账本', exact=True).click()
        expect(panel).to_have_count(0)
        page.get_by_role('button', name='每日复盘', exact=True).click()
        panel = page.get_by_role('region', name='每日对照复盘')
        field = page.get_by_label('影子复盘日期', exact=True)
        expect(field).to_have_value(yesterday)
        field.fill(today)
        expect(panel).to_have_attribute('aria-busy', 'false')
        expect(panel.locator('.sd-source')).to_contain_text(today)
        with page.expect_download() as download:
            page.get_by_role('link', name='导出当日对照报告', exact=True).click()
        target = output / 'daily-export.json'
        download.value.save_as(target)
        payload = json.loads(target.read_text())
        assert payload['date'] == today and len(payload['rows']) == 7
        assert payload['source']['records'] == 3
        for width in [1440, 900, 390, 320]:
            page.set_viewport_size({'width': width, 'height': 1000 if width > 600 else 844})
            assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'), width
            clipped = panel.locator('b,strong,p,dd,span,small').evaluate_all('items => items.filter(el => el.getClientRects().length && el.clientWidth && el.scrollWidth > el.clientWidth + 2).map(el => el.textContent)')
            assert not clipped, clipped
            page.screenshot(path=str(output / f'daily-{width}.png'), full_page=True)
        assert not errors, errors
        print({'zero_trade_reason': True, 'history_missing_data': True, 'date_preserved': True, 'export': True, 'viewports': [1440, 900, 390, 320]})
    finally:
        browser.close()
