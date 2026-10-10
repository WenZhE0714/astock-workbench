import argparse
import json
from pathlib import Path

from playwright.sync_api import expect, sync_playwright


parser = argparse.ArgumentParser()
parser.add_argument('--url', default='http://127.0.0.1:8799')
parser.add_argument('--output', default='/private/tmp/astock-timeframes-check')
args = parser.parse_args()
output = Path(args.output)
output.mkdir(exist_ok=True)

with sync_playwright() as playwright:
    browser = playwright.chromium.launch(headless=True)
    try:
        page = browser.new_page(viewport={'width':1440,'height':1100})
        page.set_default_timeout(20000)
        errors, mutations = [], []
        page.on('pageerror', lambda error: errors.append(str(error)))
        page.on('request', lambda request: mutations.append(request.url) if request.method != 'GET' else None)
        page.goto(args.url + '/?view=market&chart=daily', wait_until='networkidle')
        expect(page.get_by_role('heading', name='周线 / 日线对照', exact=True)).to_be_visible()
        expect(page.locator('.timeframe-row')).to_have_count(2)
        expect(page.locator('.timeframes-heading')).to_contain_text('同向偏多')
        expect(page.locator('[data-timeframe="1d"]')).to_contain_text('MA20 / MA60')
        expect(page.locator('[data-timeframe="1w"]')).to_contain_text('MA5 / MA10')
        for width in [1440, 390, 320]:
            page.set_viewport_size({'width':width,'height':1100 if width>600 else 844})
            page.locator('.chart-timeframes').scroll_into_view_if_needed()
            assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'), width
            overflow = page.locator('.chart-timeframes :is(button,strong,span,p,small)').evaluate_all('items => items.filter(el => el.getClientRects().length && el.clientWidth && el.scrollWidth > el.clientWidth + 2).map(el => el.textContent)')
            assert not overflow, overflow
            page.screenshot(path=str(output / f'comparison-{width}.png'), full_page=True)
            page.locator('.chart-timeframes').screenshot(path=str(output / f'comparison-detail-{width}.png'))
        page.set_viewport_size({'width':1440,'height':1100})
        page.get_by_role('tab', name='1月', exact=True).click()
        canvas = page.locator('canvas[aria-label="日K线图"]')
        canvas.scroll_into_view_if_needed()
        box = canvas.bounding_box()
        with page.expect_response(lambda response: '/api/chart-timeframes?' in response.url) as historical:
            page.mouse.move(box['x']+box['width']*.4, box['y']+box['height']*.4)
            page.mouse.down()
            page.mouse.move(box['x']+box['width']*.8, box['y']+box['height']*.4, steps=10)
            page.mouse.up()
        report = historical.value.json()
        assert report['data_date'] < '2026-09-18', report
        assert report['daily']['data_date'] <= report['data_date'] and report['weekly']['data_date'] <= report['data_date']
        expect(page.locator('.timeframes-basis')).to_contain_text('历史回看')
        expect(page.locator('.timeframes-basis')).to_contain_text(report['data_date'])
        page.screenshot(path=str(output / 'historical.png'), full_page=True)

        def mismatch(route):
            response = route.fetch()
            payload = response.json()
            payload['base_fingerprint'] = 'stale-response'
            route.fulfill(json=payload)

        page.route('**/api/chart-timeframes?*', mismatch)
        page.get_by_role('button', name='刷新周期对照', exact=True).click()
        expect(page.locator('.chart-timeframes [role="alert"]')).to_be_visible()
        expect(page.locator('.timeframe-row')).to_have_count(0)
        page.unroute('**/api/chart-timeframes?*', mismatch)
        page.get_by_role('button', name='刷新周期对照', exact=True).click()
        expect(page.locator('.timeframe-row')).to_have_count(2)

        def conflict(route):
            route.fulfill(status=409, json={'error': '图表快照已变化，请刷新结构分析后查看周期对照'})

        page.route('**/api/chart-timeframes?*', conflict)
        page.get_by_role('button', name='刷新周期对照', exact=True).click()
        expect(page.locator('.chart-timeframes [role="alert"]')).to_be_visible()
        page.unroute('**/api/chart-timeframes?*', conflict)
        with page.expect_response(lambda response: '/api/chart-analysis?' in response.url):
            page.get_by_role('button', name='刷新周期对照', exact=True).click()
        expect(page.locator('.timeframe-row')).to_have_count(2)
        expect(page.locator('.chart-timeframes [role="alert"]')).to_have_count(0)
        assert not mutations, mutations
        assert not errors, errors
        print(json.dumps({'viewports':[1440,390,320], 'historical_date':report['data_date'], 'mismatched_snapshot_hidden':True, 'conflict_retry_recovered':True, 'read_only':True}))
    finally:
        browser.close()
