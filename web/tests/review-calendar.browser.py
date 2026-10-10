import argparse
import json
from pathlib import Path

from playwright.sync_api import expect, sync_playwright


parser = argparse.ArgumentParser()
parser.add_argument('--url', default='http://127.0.0.1:8799')
parser.add_argument('--output', default='/private/tmp/astock-calendar-check')
args = parser.parse_args()
output = Path(args.output)
output.mkdir(exist_ok=True)

with sync_playwright() as playwright:
    browser = playwright.chromium.launch(headless=True)
    try:
        page = browser.new_page(viewport={'width':1440, 'height':1100})
        page.set_default_timeout(20000)
        errors = []
        mutations = []
        page.on('pageerror', lambda error: errors.append(str(error)))
        page.on('request', lambda request: mutations.append(request.url) if request.method != 'GET' else None)
        page.goto(args.url + '/?view=monitor&monitor=calendar&month=2026-09', wait_until='networkidle')
        expect(page.get_by_role('heading', name='复盘日历', exact=True)).to_be_visible()
        expect(page.locator('.calendar-kpis')).to_contain_text('0.50R')
        expect(page.locator('.calendar-grid button')).to_have_count(30)
        page.locator('[data-date="2026-09-21"]').click()
        expect(page.locator('.calendar-events>li')).to_have_count(2)
        expect(page.locator('.calendar-events')).to_contain_text('2.00R')
        expect(page.locator('.calendar-events')).to_contain_text('-1.00R')
        page.locator('.calendar-undated>summary').click()
        expect(page.locator('.calendar-undated li')).to_have_count(2)
        for width in [1440, 390, 320]:
            page.set_viewport_size({'width':width, 'height':1100 if width > 600 else 844})
            assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'), width
            overflow = page.locator('.review-calendar :is(button,strong,input,select)').evaluate_all('items => items.filter(el => el.getClientRects().length && el.clientWidth && el.scrollWidth > el.clientWidth + 2).map(el => el.textContent)')
            assert not overflow, overflow
            page.screenshot(path=str(output / f'manual-{width}.png'), full_page=True)
        page.set_viewport_size({'width':1440, 'height':1100})
        page.get_by_role('button', name='下个月', exact=True).click()
        expect(page.get_by_label('复盘月份', exact=True)).to_have_value('2026-10')
        expect(page.locator('.calendar-grid button')).to_have_count(31)
        expect(page.locator('.calendar-kpis')).not_to_contain_text('0.50R')
        page.get_by_role('button', name='上个月', exact=True).click()
        expect(page.locator('.calendar-kpis')).to_contain_text('0.50R')
        page.get_by_role('button', name='影子账户', exact=True).click()
        expect(page.locator('.calendar-kpis')).to_contain_text('88.00')
        expect(page.locator('.calendar-kpis')).not_to_contain_text('0.50R')
        expect(page.locator('.calendar-undated')).to_have_count(0)
        page.get_by_label('日历影子账户', exact=True).select_option('conservative')
        expect(page.locator('.calendar-kpis')).to_contain_text('-110.00')
        page.locator('[data-date="2026-09-21"]').click()
        expect(page.locator('.calendar-events')).to_contain_text('-110.00元')
        page.screenshot(path=str(output / 'shadow-1440.png'), full_page=True)
        with page.expect_response(lambda response: '/api/strategy/shadow?profile=conservative' in response.url) as ledger:
            page.locator('.calendar-events .icon-button').first.click()
        assert ledger.value.status == 200 and ledger.value.json()['report']['realized_profit'] == -110
        page.get_by_role('button', name='监控中心', exact=False).first.click()
        expect(page.get_by_label('日历影子账户', exact=True)).to_have_value('conservative')
        expect(page.locator('[data-date="2026-09-21"]')).to_have_attribute('aria-pressed','true')
        page.get_by_role('button', name='人工复盘', exact=True).click()
        expect(page.locator('.calendar-kpis')).to_contain_text('0.50R')
        page.locator('[data-date="2026-09-21"]').click()
        page.locator('.calendar-events .icon-button').first.click()
        expect(page.locator('.chart-saved-plan.playbook-focused-plan')).to_be_visible()
        page.get_by_role('button', name='监控中心', exact=False).first.click()
        expect(page.get_by_label('复盘月份', exact=True)).to_have_value('2026-09')
        expect(page.locator('[data-date="2026-09-21"]')).to_have_attribute('aria-pressed','true')
        page.locator('[data-date="2026-09-21"]').focus()
        page.keyboard.press('ArrowRight')
        expect(page.locator('[data-date="2026-09-22"]')).to_be_focused()
        assert not mutations, mutations
        assert not errors, errors
        print(json.dumps({'manual_exit_r':0.5, 'undated':2, 'balanced_profit':88, 'conservative_profit':-110, 'viewports':[1440,390,320], 'read_only':True}))
    finally:
        browser.close()
