import argparse
import json
from pathlib import Path

from playwright.sync_api import expect, sync_playwright

parser = argparse.ArgumentParser()
parser.add_argument('--url', default='http://127.0.0.1:8799')
parser.add_argument('--output', default='/private/tmp/astock-pattern-validation-check')
args = parser.parse_args()
output = Path(args.output)
output.mkdir(parents=True, exist_ok=True)

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    try:
        page = browser.new_page(viewport={'width':1440,'height':1100})
        page.set_default_timeout(20000)
        errors, mutations = [], []
        page.on('pageerror', lambda error: errors.append(str(error)))
        page.on('request', lambda request: mutations.append(request.url) if request.method != 'GET' else None)
        page.goto(args.url+'/?view=strategy&strategy=patterns', wait_until='networkidle')
        expect(page.get_by_role('heading', name='形态验证', exact=True)).to_be_visible()
        page.get_by_label('验证股票池', exact=True).fill('sh600004, sh600007')
        page.get_by_label('验证开始日期', exact=True).fill('2026-06-01')
        page.get_by_label('验证结束日期', exact=True).fill('2026-09-25')
        assert page.locator('.pv-form').evaluate('form => form.checkValidity()')
        with page.expect_response(lambda r: r.url.endswith('/api/pattern-validation') and r.request.method == 'POST', timeout=60000) as run:
            page.get_by_role('button', name='运行验证', exact=True).click()
        assert run.value.status == 200, run.value.text()
        result = run.value.json()
        run_id = result['report']['run_id']
        assert result['totals']['mature'] >= 2
        expect(page.locator('.pv-group-table tbody tr').first).to_be_visible()
        page.get_by_label('筛选验证形态', exact=True).select_option('head-shoulders-bottom')
        page.get_by_role('button', name='20日', exact=True).click()
        expect(page.locator('.pv-sample')).to_have_count(1)
        page.locator('.pv-sample > summary').click()
        expect(page.locator('.pv-outcome')).to_have_count(3)
        sample = next(item for item in result['samples'] if item['pattern_id'] == 'head-shoulders-bottom')
        page.get_by_role('button', name='查看识别日 '+sample['id'], exact=True).click()
        expect(page.locator('.chart-analysis-date')).to_contain_text(sample['observed_on'])
        expect(page.get_by_role('combobox', name='观察形态', exact=True)).to_have_value('head-shoulders-bottom')
        canvas = page.locator('canvas[aria-label="日K线图"]')
        assert canvas.evaluate('c => { const d=c.getContext("2d").getImageData(0,0,c.width,c.height).data; return new Set(Array.from(d).filter((v,i)=>i%4===0)).size > 10; }')
        page.get_by_role('button', name='返回形态验证', exact=True).click()
        expect(page.get_by_label('筛选验证形态', exact=True)).to_have_value('head-shoulders-bottom')
        expect(page.get_by_role('button', name='20日', exact=True)).to_have_attribute('aria-pressed','true')
        expect(page.locator('.pv-sample')).to_have_count(1)
        page.locator('.pv-sample > summary').click()
        for width in [1440,390,320]:
            page.set_viewport_size({'width':width,'height':1100 if width>600 else 844})
            assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'), width
            overflow = page.locator('.pattern-validation :is(button,strong,span,p,small,td)').evaluate_all('items=>items.filter(el=>el.getClientRects().length && el.clientWidth && el.scrollWidth > el.clientWidth+2).map(el=>el.textContent)')
            assert not overflow, overflow
            page.screenshot(path=str(output / f'validation-{width}.png'), full_page=True)
        page.set_viewport_size({'width':1440,'height':1100})
        with page.expect_download() as download:
            page.get_by_role('link', name='导出完整验证归档', exact=True).click()
        exported = json.loads(Path(download.value.path()).read_text())
        assert len(exported['inputs']) == 3 and exported['input_hash'] == result['report']['input_hash']
        page.get_by_label('筛选验证方向', exact=True).select_option('bearish')
        expect(page.locator('.pv-sample')).to_have_count(0)
        expect(page.locator('.pv-totals')).to_contain_text('--')
        page.reload(wait_until='networkidle')
        page.get_by_label('形态验证归档', exact=True).select_option(run_id)
        expect(page.locator('.pv-sample').first).to_be_visible()
        expect(page.get_by_label('验证开始日期', exact=True)).to_have_value('2026-06-01')
        expect(page.get_by_label('验证结束日期', exact=True)).to_have_value('2026-09-25')

        def failure(route):
            route.fulfill(status=503,json={'error':'测试读取失败'})
        page.route('**/api/pattern-validation?*', failure)
        page.get_by_role('button', name='10日', exact=True).click()
        expect(page.get_by_role('alert')).to_contain_text('测试读取失败')
        expect(page.locator('.pv-sample')).to_have_count(0)
        page.unroute('**/api/pattern-validation?*', failure)
        page.get_by_role('button', name='重试读取', exact=True).click()
        expect(page.locator('.pv-sample').first).to_be_visible()
        page.get_by_role('tab', name='策略回测', exact=True).click()
        expect(page.get_by_role('heading', name='候选生命周期与人工批准', exact=True)).to_be_visible()
        page.get_by_role('tab', name='形态验证', exact=True).click()
        expect(page.locator('.pv-sample').first).to_be_visible()

        pending = []
        page.route('**/api/pattern-validation', lambda route: pending.append(route) if route.request.method == 'POST' else route.continue_())
        page.get_by_role('button', name='运行验证', exact=True).click()
        page.get_by_role('button', name='取消验证', exact=True).click()
        expect(page.get_by_role('alert')).to_contain_text('验证已取消')
        for route in pending:
            route.abort()
        page.unroute('**/api/pattern-validation')
        assert all(url.endswith('/api/pattern-validation') for url in mutations), mutations
        assert not errors, errors
        print(json.dumps({'run_id':run_id,'mature':result['totals']['mature'],'viewports':[1440,390,320],'archive_export':True,'historical_jump_and_return':True,'read_failure_recovery':True,'cancel':True}))
    finally:
        browser.close()
