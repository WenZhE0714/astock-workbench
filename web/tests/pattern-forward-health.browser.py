import argparse
from pathlib import Path
from playwright.sync_api import expect, sync_playwright

expect.set_options(timeout=30000)
parser = argparse.ArgumentParser()
parser.add_argument('--url', required=True)
parser.add_argument('--output', default='/private/tmp/astock-forward-health-browser-check')
args = parser.parse_args()
output = Path(args.output)
output.mkdir(parents=True, exist_ok=True)

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    try:
        page = browser.new_page(viewport={'width': 1440, 'height': 1100})
        page.set_default_timeout(30000)
        errors = []
        page.on('pageerror', lambda error: errors.append(str(error)))
        page.goto(args.url+'/?view=strategy&strategy=patterns', wait_until='networkidle')
        page.get_by_role('button', name='前向观察', exact=True).click()
        page.get_by_label('前向任务名称', exact=True).fill('前向运行验收')
        page.get_by_label('前向价格口径', exact=True).select_option('raw')
        with page.expect_response(lambda r: r.url.endswith('/api/pattern-forwards') and r.request.method == 'POST') as created:
            page.get_by_role('button', name='创建前向任务', exact=True).click()
        task = created.value.json()['task']
        page.get_by_role('button', name='运行监控', exact=True).click()
        expect(page.locator('[data-health-phase]')).to_have_attribute('data-health-phase', 'waiting')
        page.get_by_role('button', name='校验前向归档', exact=True).click()
        expect(page.locator('[data-audit-state]')).to_have_attribute('data-audit-state', 'waiting')
        page.request.post(args.url+'/__test/forward-clock?step=calendar-fail')
        expect(page.locator('.pv-health-event[data-alert-kind="opened"]')).to_have_count(1)
        page.get_by_role('button', name='确认记录1', exact=True).click()
        expect(page.get_by_role('button', name='确认记录1', exact=True)).to_have_count(0)
        page.request.post(args.url+'/__test/forward-clock?step=calendar-recover')
        expect(page.locator('.pv-health-event[data-alert-kind="resolved"]')).to_have_count(1)
        page.get_by_label('仅未确认', exact=True).check()
        expect(page.locator('.pv-health-event')).to_have_count(1)
        page.get_by_label('仅未确认', exact=True).uncheck()
        date = page.request.post(args.url+'/__test/forward-clock?step=partial').json()['date']
        expect(page.locator('[data-health-phase]')).to_have_attribute('data-health-phase', 'retrying')
        expect(page.get_by_label(date+'采集进度', exact=True)).to_have_attribute('value', '1')
        expect(page.locator('.pv-health-missing')).to_contain_text('sh600101')
        page.request.post(args.url+'/__test/forward-clock?step=retry')
        expect(page.locator('[data-health-phase]')).to_have_attribute('data-health-phase', 'complete')
        expect(page.get_by_label(date+'采集进度', exact=True)).to_have_attribute('value', '2')
        expect(page.locator('[data-audit-state]')).to_have_attribute('data-audit-state', 'verified')
        page.get_by_role('button', name='查看'+date+'快照', exact=True).click()
        expect(page.get_by_label('前向采集日', exact=True)).to_have_value(date)
        page.get_by_role('button', name='运行监控', exact=True).click()
        page.request.post(args.url+'/__test/forward-clock?step=later')
        expect(page.locator('.pv-health-day')).to_have_count(4)
        expect(page.locator('[data-audit-state]')).to_have_attribute('data-audit-state', 'partial')
        for width in [1440, 390, 320]:
            page.set_viewport_size({'width': width, 'height': 1100 if width > 600 else 844})
            assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'), width
            overflow = page.locator('.pv-forward-health :is(button,strong,span,p,small,dd)').evaluate_all('items=>items.filter(el=>el.getClientRects().length && el.clientWidth && el.scrollWidth>el.clientWidth+2).map(el=>el.textContent)')
            assert not overflow, overflow
            page.screenshot(path=str(output/f'health-{width}.png'), full_page=True)
        page.reload(wait_until='networkidle')
        page.get_by_role('button', name='前向观察', exact=True).click()
        page.get_by_role('button', name='运行监控', exact=True).click()
        expect(page.locator('.pv-health-day')).to_have_count(4)
        expect(page.get_by_role('button', name='确认记录1', exact=True)).to_have_count(0)
        assert not errors, errors
        print({'calendar_recovery': True, 'persistent_acknowledgment': True, 'partial_retry': True, 'archive_audit': True, 'missed_days': 2, 'viewports': [1440, 390, 320]})
    finally:
        browser.close()
