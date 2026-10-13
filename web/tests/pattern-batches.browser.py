import argparse
from pathlib import Path
from playwright.sync_api import expect, sync_playwright

parser = argparse.ArgumentParser()
parser.add_argument('--url', required=True)
parser.add_argument('--output', default='/private/tmp/astock-batch-browser-check')
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
        page.get_by_role('button', name='批量验证', exact=True).click()
        page.get_by_role('button', name='跨行业20只示例', exact=True).click()
        with page.expect_response(lambda r: r.url.endswith('/api/pattern-pools') and r.request.method == 'POST') as saved:
            page.get_by_role('button', name='保存股票池版本', exact=True).click()
        assert saved.value.status == 201, saved.value.text()
        pool = saved.value.json()
        assert len(pool['members']) == 20 and len(pool['version']) == 64
        page.get_by_role('button', name='编辑为新版本', exact=True).click()
        page.get_by_label('股票池名称', exact=True).fill('新版研究名单')
        with page.expect_response(lambda r: r.url.endswith('/api/pattern-pools') and r.request.method == 'POST') as changed:
            page.get_by_role('button', name='保存股票池版本', exact=True).click()
        assert changed.value.status == 201 and changed.value.json()['version'] != pool['version']
        page.get_by_label('冻结股票池版本', exact=True).select_option(pool['version'])
        page.get_by_label('批量开始日期', exact=True).fill('2026-06-01')
        page.get_by_label('批量结束日期', exact=True).fill('2026-09-25')
        page.get_by_label('批量价格口径', exact=True).select_option('raw')
        with page.expect_response(lambda r: r.url.endswith('/api/pattern-batches') and r.request.method == 'POST') as started:
            page.get_by_role('button', name='启动批量验证', exact=True).click()
        assert started.value.status == 202, started.value.text()
        job_id = started.value.json()['id']
        page.reload(wait_until='networkidle')
        page.get_by_role('button', name='批量验证', exact=True).click()
        page.get_by_label('批量任务', exact=True).select_option(job_id)
        expect(page.locator('.pv-job')).to_have_attribute('data-job-status', 'partial', timeout=60000)
        expect(page.locator('.pv-job')).to_contain_text('失败 1')
        page.get_by_role('button', name='重试失败项', exact=True).click()
        expect(page.locator('.pv-job')).to_have_attribute('data-job-status', 'completed', timeout=60000)
        expect(page.locator('.pv-job')).to_contain_text('检查点 20 / 20')
        page.get_by_role('button', name='查看批量归档 2', exact=True).click()
        expect(page.get_by_role('region', name='股票池样本分布', exact=True)).to_be_visible()
        expect(page.locator('[data-distribution-symbol]')).to_have_count(20)
        expect(page.locator('.pv-pool-distribution')).to_contain_text(pool['version'][:8])
        page.locator('.pv-pool-distribution details > summary').click()
        for width in [1440, 390, 320]:
            page.set_viewport_size({'width':width,'height':1100 if width>600 else 844})
            assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'), width
            overflow = page.locator('.pattern-validation :is(button,strong,span,p,small,td)').evaluate_all('items=>items.filter(el=>el.getClientRects().length && el.clientWidth && el.scrollWidth>el.clientWidth+2).map(el=>el.textContent)')
            assert not overflow, overflow
            page.screenshot(path=str(output/f'batch-{width}.png'), full_page=True)
        page.set_viewport_size({'width':1440,'height':1100})
        page.get_by_label('批量开始日期', exact=True).fill('2026-06-01')
        page.get_by_label('批量结束日期', exact=True).fill('2026-09-25')
        with page.expect_response(lambda r: r.url.endswith('/api/pattern-batches') and r.request.method == 'POST') as second:
            page.get_by_role('button', name='启动批量验证', exact=True).click()
        assert second.value.status == 202, second.value.text()
        second_id = second.value.json()['id']
        page.get_by_role('button', name='取消任务', exact=True).click()
        expect(page.locator('.pv-job')).to_have_attribute('data-job-status', 'cancelled')
        page.reload(wait_until='networkidle')
        page.get_by_role('button', name='批量验证', exact=True).click()
        page.get_by_label('批量任务', exact=True).select_option(second_id)
        page.get_by_role('button', name='继续任务', exact=True).click()
        expect(page.locator('.pv-job')).to_have_attribute('data-job-status', 'completed', timeout=60000)
        assert not errors, errors
        print({'pool_versions':2,'stocks':20,'retry_failed':True,'reload_recovery':True,'cancel_resume':True,'viewports':[1440,390,320]})
    finally:
        browser.close()
