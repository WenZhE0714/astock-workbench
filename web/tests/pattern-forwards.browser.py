import argparse
import json
from pathlib import Path
from playwright.sync_api import expect, sync_playwright

expect.set_options(timeout=30000)

parser = argparse.ArgumentParser()
parser.add_argument('--url', required=True)
parser.add_argument('--output', default='/private/tmp/astock-forward-browser-check')
args = parser.parse_args()
output = Path(args.output)
output.mkdir(parents=True, exist_ok=True)

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    try:
        page = browser.new_page(viewport={'width':1440,'height':1100})
        page.set_default_timeout(30000)
        errors = []
        page.on('pageerror', lambda e: errors.append(str(e)))
        page.goto(args.url+'/?view=strategy&strategy=patterns', wait_until='networkidle')
        page.get_by_role('button', name='前向观察', exact=True).click()
        expect(page.get_by_role('heading', name='前向研究任务', exact=True)).to_be_visible()
        expect(page.locator('.pv-archive')).to_have_count(0)
        page.get_by_label('前向任务名称', exact=True).fill('冻结前向样本')
        page.get_by_label('前向价格口径', exact=True).select_option('raw')
        with page.expect_response(lambda r: r.url.endswith('/api/pattern-forwards') and r.request.method=='POST') as created:
            page.get_by_role('button', name='创建前向任务', exact=True).click()
        assert created.value.status == 201, created.value.text()
        task = created.value.json()['task']
        expect(page.get_by_role('button', name='检查收盘采集', exact=True)).to_be_disabled()
        expect(page.get_by_label('前向采集日', exact=True)).to_have_value('')
        clock = page.request.post(args.url+'/__test/forward-clock?step=close').json()
        page.get_by_role('button', name='刷新前向任务', exact=True).click()
        page.get_by_role('button', name='检查收盘采集', exact=True).click()
        expect(page.locator('.pv-forward-day')).to_have_attribute('data-forward-day-state','captured')
        expect(page.locator('.pv-observation[data-observation-state="captured"]')).to_have_count(2)
        page.locator('.pv-observation > summary').first.click()
        expect(page.locator('.pv-forward-sample').first).to_be_visible()
        for width in [1440,390,320]:
            page.set_viewport_size({'width':width,'height':1100 if width>600 else 844})
            assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'), width
            overflow = page.locator('.pv-forward :is(button,strong,span,p,small)').evaluate_all('items=>items.filter(el=>el.getClientRects().length && el.clientWidth && el.scrollWidth>el.clientWidth+2).map(el=>el.textContent)')
            assert not overflow, overflow
            page.screenshot(path=str(output/f'forward-{width}.png'), full_page=True)
        page.set_viewport_size({'width':1440,'height':1100})
        with page.expect_download() as download:
            page.get_by_role('link', name='导出当日快照', exact=True).click()
        exported = json.loads(Path(download.value.path()).read_text())
        assert exported['task']['config_hash'] == task['config_hash']
        assert exported['day']['date'] == clock['date']
        assert len(exported['stocks']) == 2 and len(exported['stocks'][0]['input']['bars']) > 120
        assert all(s['observed_on']==clock['date'] and not s.get('outcomes') for stock in exported['stocks'] for s in stock['samples'])
        day_hashes = [stock['hash'] for stock in exported['stocks']]
        page.get_by_role('button', name='暂停前向任务', exact=True).click()
        expect(page.locator('.pv-forward-status')).to_have_attribute('data-forward-enabled','false')
        page.get_by_role('button', name='启用前向任务', exact=True).click()
        expect(page.locator('.pv-forward-status')).to_have_attribute('data-forward-enabled','true')
        page.request.post(args.url+'/__test/forward-clock?step=later')
        page.get_by_role('button', name='刷新前向任务', exact=True).click()
        expect(page.get_by_label('前向采集日', exact=True).locator('option')).to_have_count(5)
        options = page.get_by_label('前向采集日', exact=True).locator('option').all_text_contents()
        assert sum('缺采' in option for option in options) == 2, options
        page.get_by_label('前向采集日', exact=True).select_option(clock['date'])
        with page.expect_download() as download:
            page.get_by_role('link', name='导出当日快照', exact=True).click()
        reloaded = json.loads(Path(download.value.path()).read_text())
        assert [stock['hash'] for stock in reloaded['stocks']] == day_hashes
        page.reload(wait_until='networkidle')
        page.get_by_role('button', name='前向观察', exact=True).click()
        page.get_by_label('前向研究任务', exact=True).select_option(task['id'])
        expect(page.get_by_label('前向采集日', exact=True).locator('option')).to_have_count(5)
        assert not errors, errors
        print({'task_created':True,'before_close_blocked':True,'immutable_snapshots':2,'missed_days':2,'pause_resume':True,'viewports':[1440,390,320]})
    finally:
        browser.close()
