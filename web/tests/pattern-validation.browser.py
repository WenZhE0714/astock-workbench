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
        assert result['report']['benchmark']['symbol'] == 'sh000300'
        assert result['totals']['benchmark_paired'] == result['totals']['mature']
        expect(page.get_by_role('heading', name='同期基准对照', exact=True)).to_be_visible()
        expect(page.locator('.pv-group-table tbody tr').first).to_be_visible()
        coverage = page.locator('[data-coverage-symbol="sh600007"]')
        expect(coverage).to_contain_text('区间取数')
        expect(coverage).to_contain_text('覆盖有缺口')
        expect(coverage).to_contain_text('原因未确认')
        coverage.locator('.pv-gaps > summary').click()
        expect(coverage.locator('.pv-gaps p')).to_be_visible()
        page.get_by_label('筛选验证形态', exact=True).select_option('head-shoulders-bottom')
        page.get_by_role('button', name='20日', exact=True).click()
        expect(page.locator('.pv-sample')).to_have_count(1)
        expect(page.locator('.pv-action-filter')).to_contain_text('事件隔离 1')
        expect(page.locator('[data-actions-symbol="sh600004"]')).to_contain_text('现金分红')
        page.get_by_label('筛选公司行动', exact=True).select_option('clear')
        expect(page.locator('.pv-sample')).to_have_count(0)
        page.get_by_label('筛选公司行动', exact=True).select_option('affected')
        expect(page.locator('.pv-sample')).to_have_count(1)
        page.locator('.pv-sample > summary').click()
        expect(page.locator('.pv-outcome')).to_have_count(3)
        expect(page.locator('.pv-outcome [data-action-state="affected"]')).to_have_count(2)
        expect(page.locator('.pv-outcome [data-action-state="clear"]')).to_have_count(1)
        expect(page.locator('.pv-benchmark-outcome[data-benchmark-state="matched"]')).to_have_count(3)
        expect(page.locator('.pv-benchmark')).to_contain_text('0 / 0')
        sample = next(item for item in result['samples'] if item['pattern_id'] == 'head-shoulders-bottom')
        page.get_by_role('button', name='查看识别日 '+sample['id'], exact=True).click()
        expect(page.locator('.chart-analysis-date')).to_contain_text(sample['observed_on'])
        expect(page.get_by_role('combobox', name='观察形态', exact=True)).to_have_value('head-shoulders-bottom')
        canvas = page.locator('canvas[aria-label="日K线图"]')
        assert canvas.evaluate('c => { const d=c.getContext("2d").getImageData(0,0,c.width,c.height).data; return new Set(Array.from(d).filter((v,i)=>i%4===0)).size > 10; }')
        page.get_by_role('button', name='返回形态验证', exact=True).click()
        expect(page.get_by_label('筛选验证形态', exact=True)).to_have_value('head-shoulders-bottom')
        expect(page.get_by_role('button', name='20日', exact=True)).to_have_attribute('aria-pressed','true')
        expect(page.get_by_label('筛选公司行动', exact=True)).to_have_value('affected')
        expect(page.locator('.pv-sample')).to_have_count(1)
        page.locator('.pv-sample > summary').click()
        page.locator('[data-coverage-symbol="sh600007"] .pv-gaps > summary').click()
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
        assert exported['inputs'][0]['fetch_mode'] == 'range'
        assert exported['inputs'][0]['fetch_end'] == '2026-09-25'
        assert exported['inputs'][0]['actions']['events'][0]['id'] == 'browser-dividend'
        assert exported['benchmark']['price_basis'] == 'price-index'
        exported_sample = next(item for item in exported['samples'] if item['id'] == sample['id'])
        assert all(item['benchmark']['state'] == 'matched' for item in exported_sample['outcomes'])
        page.get_by_label('筛选验证方向', exact=True).select_option('bearish')
        expect(page.locator('.pv-sample')).to_have_count(0)
        expect(page.locator('.pv-totals').first).to_contain_text('--')
        expect(page.locator('.pv-benchmark')).to_contain_text('--')
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

        page.get_by_label('验证价格口径', exact=True).select_option('hfq')
        expect(page.locator('.pv-basis')).to_contain_text('未复权')
        with page.expect_response(lambda r: r.url.endswith('/api/pattern-validation') and r.request.method == 'POST', timeout=60000) as adjusted_run:
            page.get_by_role('button', name='运行验证', exact=True).click()
        assert adjusted_run.value.status == 200, adjusted_run.value.text()
        adjusted_result = adjusted_run.value.json()
        assert adjusted_result['report']['version'] == 'pattern-validation-v8'
        assert all(item['quality_status'] == 'passed' for item in adjusted_result['report']['adjustments'])
        assert adjusted_result['report']['adjustments'][1]['quality'][0]['share_ratio'] == 1.5
        expect(page.locator('.pv-basis')).to_contain_text('后复权研究')
        expect(page.locator('[data-adjustment-symbol="sh600004"]')).to_contain_text('已应用')
        expect(page.locator('[data-adjustment-symbol="sh600004"]')).to_contain_text('理论除权')
        expect(page.locator('[data-adjustment-symbol="sh600007"]')).to_contain_text('股数比例 1.5000')
        expect(page.locator('.pv-quality-list [data-quality-state="passed"]')).to_have_count(2)
        page.get_by_label('筛选验证形态', exact=True).select_option('head-shoulders-bottom')
        page.get_by_role('button', name='20日', exact=True).click()
        page.get_by_label('筛选公司行动', exact=True).select_option('adjusted')
        expect(page.locator('.pv-sample')).to_have_count(1)
        page.locator('.pv-sample > summary').click()
        expect(page.locator('.pv-outcome [data-action-state="adjusted"]')).to_have_count(2)
        adjusted_sample = next(item for item in adjusted_result['samples'] if item['pattern_id'] == 'head-shoulders-bottom')
        comparison = next(item for item in adjusted_sample['outcomes'] if item['horizon'] == 20)['benchmark']
        expect(page.locator('.pv-benchmark')).to_contain_text('1 / 1')
        expect(page.locator('.pv-benchmark')).to_contain_text(f"{comparison['return']:.2f}%")
        expect(page.locator('.pv-benchmark')).to_contain_text(f"{comparison['excess_return']:.2f}")
        expect(page.locator('.pv-benchmark-outcome').last).to_contain_text('个百分点')
        for width in [1440,390,320]:
            page.set_viewport_size({'width':width,'height':1100 if width>600 else 844})
            assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'), width
            overflow = page.locator('.pattern-validation :is(button,strong,span,p,small,td)').evaluate_all('items=>items.filter(el=>el.getClientRects().length && el.clientWidth && el.scrollWidth > el.clientWidth+2).map(el=>el.textContent)')
            assert not overflow, overflow
            page.screenshot(path=str(output / f'adjusted-{width}.png'), full_page=True)
        page.set_viewport_size({'width':1440,'height':1100})
        page.get_by_role('button', name='查看识别日 '+adjusted_sample['id'], exact=True).click()
        expect(page.locator('.pv-chart-return')).to_contain_text('未复权价格')
        page.get_by_role('button', name='返回形态验证', exact=True).click()
        expect(page.get_by_label('验证价格口径', exact=True)).to_have_value('hfq')
        expect(page.get_by_label('筛选公司行动', exact=True)).to_have_value('adjusted')
        with page.expect_download() as download:
            page.get_by_role('link', name='导出完整验证归档', exact=True).click()
        adjusted_export = json.loads(Path(download.value.path()).read_text())
        assert adjusted_export['request']['price_basis'] == 'hfq'
        assert adjusted_export['inputs'][0]['bars'] == exported['inputs'][0]['bars']
        assert len(adjusted_export['inputs'][0]['adjustment']['factors']) == 2
        assert len(adjusted_export['adjustments'][0]['factor_version']) == 64
        assert len(adjusted_export['adjustments'][0]['volume_version']) == 64
        assert adjusted_export['inputs'][0]['actions']['events'][0]['terms']['cash_per_10'] == 2.5
        page.reload(wait_until='networkidle')
        page.get_by_label('形态验证归档', exact=True).select_option(adjusted_export['run_id'])
        expect(page.get_by_label('验证价格口径', exact=True)).to_have_value('hfq')
        expect(page.locator('.pv-basis')).to_contain_text('后复权研究')

        def mismatch(route):
            payload = json.loads(json.dumps(adjusted_result))
            item = payload['report']['adjustments'][0]
            item['quality_status'] = 'unverified'
            item['quality'][0].update(state='mismatch', detail='理论除权价与因子隐含除权价偏差超过0.01元', price_error=0.02)
            route.fulfill(status=200, json=payload)
        page.route('**/api/pattern-validation?*', mismatch)
        page.get_by_role('button', name='10日', exact=True).click()
        expect(page.locator('[data-quality-state="mismatch"]')).to_contain_text('数值不一致')
        expect(page.locator('[data-quality-state="mismatch"]')).to_contain_text('0.0200')
        page.unroute('**/api/pattern-validation?*', mismatch)

        def legacy_benchmark(route):
            payload = json.loads(json.dumps(adjusted_result))
            payload['report']['version'] = 'pattern-validation-v5'
            payload['report'].pop('benchmark', None)
            for sample in payload['samples']:
                for outcome in sample['outcomes']:
                    outcome.pop('benchmark', None)
            payload['totals'].update(benchmark_paired=0, benchmark_unpaired=payload['totals']['mature'], average_benchmark_return=None, average_paired_price_return=None, average_excess_return=None, average_direction_excess_return=None, positive_excess_rate=None, positive_direction_excess_rate=None)
            route.fulfill(status=200, json=payload)
        page.route('**/api/pattern-validation?*', legacy_benchmark)
        page.get_by_role('button', name='20日', exact=True).click()
        expect(page.locator('.pv-benchmark')).to_contain_text('旧归档未记录同期基准收益')
        expect(page.locator('.pv-benchmark')).to_contain_text('--')
        page.unroute('**/api/pattern-validation?*', legacy_benchmark)

        page.get_by_label('分期评估', exact=True).check()
        page.get_by_label('验证期起点', exact=True).fill('2026-08-31')
        with page.expect_response(lambda r: r.url.endswith('/api/pattern-validation') and r.request.method == 'POST', timeout=60000) as study_run:
            page.get_by_role('button', name='运行验证', exact=True).click()
        assert study_run.value.status == 200, study_run.value.text()
        study_result = study_run.value.json()
        assert study_result['filter']['period'] == 'validation'
        assert study_result['report']['study']['kind'] == 'retrospective'
        assert len(study_result['report']['study']['config_hash']) == 64
        expect(page.get_by_role('button', name='验证期', exact=True)).to_have_attribute('aria-pressed', 'true')
        expect(page.locator('[data-study-period="research"]')).to_be_visible()
        expect(page.locator('[data-study-period="validation"]')).to_be_visible()
        page.get_by_role('button', name='研究期', exact=True).click()
        page.get_by_label('筛选验证形态', exact=True).select_option('head-shoulders-bottom')
        page.get_by_role('button', name='20日', exact=True).click()
        expect(page.locator('.pv-sample')).to_have_count(1)
        page.locator('.pv-sample > summary').click()
        expect(page.locator('[data-period-state="excluded"]')).to_have_count(2)
        expect(page.locator('.pv-basis')).to_contain_text('数据截止 2026-08-30')
        expect(page.locator('[data-study-period="research"]')).to_contain_text('--')
        page.locator('.pv-frozen-config > summary').click()
        for width in [1440,390,320]:
            page.set_viewport_size({'width':width,'height':1100 if width>600 else 844})
            assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'), width
            overflow = page.locator('.pattern-validation :is(button,strong,span,p,small,td)').evaluate_all('items=>items.filter(el=>el.getClientRects().length && el.clientWidth && el.scrollWidth > el.clientWidth+2).map(el=>el.textContent)')
            assert not overflow, overflow
            page.screenshot(path=str(output / f'study-{width}.png'), full_page=True)
        page.set_viewport_size({'width':1440,'height':1100})
        page.get_by_role('button', name='查看识别日 '+adjusted_sample['id'], exact=True).click()
        page.get_by_role('button', name='返回形态验证', exact=True).click()
        expect(page.get_by_role('button', name='研究期', exact=True)).to_have_attribute('aria-pressed', 'true')
        expect(page.get_by_label('验证期起点', exact=True)).to_have_value('2026-08-31')
        with page.expect_download() as download:
            page.get_by_role('link', name='导出完整验证归档', exact=True).click()
        study_export = json.loads(Path(download.value.path()).read_text())
        assert study_export['study']['config_hash'] == study_result['report']['study']['config_hash']
        assert study_export['request']['validation_start'] == '2026-08-31'
        assert all(s['confirmed_on'] < '2026-08-31' for s in study_export['samples'] if s['period'] == 'research' and s.get('confirmed_on'))
        page.get_by_label('验证期起点', exact=True).fill(adjusted_sample['observed_on'])
        with page.expect_response(lambda r: r.url.endswith('/api/pattern-validation') and r.request.method == 'POST', timeout=60000) as earlier_study:
            page.get_by_role('button', name='运行验证', exact=True).click()
        assert earlier_study.value.status == 200, earlier_study.value.text()
        assert earlier_study.value.json()['report']['study']['config_hash'] != study_export['study']['config_hash']
        expect(page.get_by_role('button', name='验证期', exact=True)).to_have_attribute('aria-pressed', 'true')
        expect(page.locator('.pv-sample').first).to_be_visible()
        page.reload(wait_until='networkidle')
        page.get_by_label('形态验证归档', exact=True).select_option(study_export['run_id'])
        expect(page.get_by_label('验证期起点', exact=True)).to_have_value('2026-08-31')
        expect(page.get_by_role('button', name='验证期', exact=True)).to_have_attribute('aria-pressed', 'true')
        page.get_by_label('形态验证归档', exact=True).select_option(run_id)
        expect(page.get_by_label('分期评估', exact=True)).not_to_be_checked()
        expect(page.locator('.pv-study')).to_have_count(0)

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
