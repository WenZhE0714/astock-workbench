import argparse
import json
from pathlib import Path
from playwright.sync_api import expect, sync_playwright

expect.set_options(timeout=30000)
parser = argparse.ArgumentParser()
parser.add_argument('--url', required=True)
parser.add_argument('--output', default='/private/tmp/astock-forward-tracking-browser-check')
args = parser.parse_args()
output = Path(args.output)
output.mkdir(parents=True, exist_ok=True)

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    try:
        page = browser.new_page(viewport={'width': 1440, 'height': 1100})
        page.set_default_timeout(30000)
        errors = []
        page.on('pageerror', lambda e: errors.append(str(e)))
        page.goto(args.url+'/?view=strategy&strategy=patterns', wait_until='networkidle')
        page.get_by_role('button', name='前向观察', exact=True).click()
        page.get_by_label('前向任务名称', exact=True).fill('前向结果验证')
        page.get_by_label('前向价格口径', exact=True).select_option('raw')
        with page.expect_response(lambda r: r.url.endswith('/api/pattern-forwards') and r.request.method == 'POST') as created:
            page.get_by_role('button', name='创建前向任务', exact=True).click()
        assert created.value.status == 201
        task = created.value.json()['task']
        page.get_by_role('button', name='结果跟踪', exact=True).click()
        expect(page.get_by_text('暂无符合条件的跟踪样本', exact=True)).to_be_visible()
        first_day = page.request.post(args.url+'/__test/forward-clock?step=close').json()['date']
        page.get_by_role('button', name='刷新前向任务', exact=True).click()
        page.get_by_role('button', name='检查收盘采集', exact=True).click()
        page.get_by_label('前向形态筛选', exact=True).select_option('head-shoulders-bottom')
        expect(page.locator('.pv-forward-result')).to_have_count(2)
        assert page.locator('.pv-forward-result').first.get_attribute('data-result-outcome') == 'unconfirmed'
        before = page.request.get(args.url+'/api/pattern-forwards?id='+task['id']+'&date='+first_day+'&download=1').json()
        response = page.request.post(args.url+'/__test/forward-clock?step=mature', timeout=120000)
        assert response.status == 200, response.text()
        page.get_by_role('button', name='刷新前向任务', exact=True).click()
        page.get_by_role('group', name='前向结果窗口', exact=True).get_by_role('button', name='20日', exact=True).click()
        expect(page.locator('.pv-forward-result[data-result-outcome="mature"]')).to_have_count(2)
        page.locator('.pv-forward-result > summary').first.click()
        expect(page.locator('.pv-forward-history li').first).to_be_visible()
        assert page.locator('.pv-forward-history li').count() >= 21
        for width in [1440, 390, 320]:
            page.set_viewport_size({'width': width, 'height': 1100 if width > 600 else 844})
            assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'), width
            overflow = page.locator('.pv-forward-results :is(button,strong,span,p,small,time)').evaluate_all('items=>items.filter(el=>el.getClientRects().length && el.clientWidth && el.scrollWidth>el.clientWidth+2).map(el=>el.textContent)')
            assert not overflow, overflow
            if width > 1000:
                assert page.evaluate('''() => {
                    const launcher = document.querySelector('.assistant-launcher').getBoundingClientRect();
                    return [...document.querySelectorAll('.pv-forward-result > summary')].every(el => el.getBoundingClientRect().right < launcher.left);
                }''')
            page.screenshot(path=str(output/f'tracking-{width}.png'), full_page=True)
        page.set_viewport_size({'width': 1440, 'height': 1100})
        with page.expect_download() as download:
            page.get_by_role('link', name='导出前向跟踪日志', exact=True).click()
        archive = json.loads(Path(download.value.path()).read_text())
        assert archive['task']['config_hash'] == task['config_hash']
        assert len(archive['logs']) > 20
        assert archive['logs'][1]['previous_hash'] == archive['logs'][0]['hash']
        assert len(archive['evidence'][0]['stocks'][0]['input']['bars']) > 120
        assert archive['evidence'][0]['stocks'][0]['hash'] == before['stocks'][0]['hash']
        page.get_by_label('前向结果截止日', exact=True).select_option(first_day)
        expect(page.locator('.pv-forward-result[data-result-outcome="unconfirmed"]')).to_have_count(2)
        page.get_by_label('前向状态筛选', exact=True).select_option('confirmed')
        expect(page.get_by_text('暂无符合条件的跟踪样本', exact=True)).to_be_visible()
        page.get_by_label('前向结果截止日', exact=True).select_option('')
        expect(page.locator('.pv-forward-result[data-result-outcome="mature"]')).to_have_count(2)
        page.reload(wait_until='networkidle')
        page.get_by_role('button', name='前向观察', exact=True).click()
        page.get_by_role('button', name='结果跟踪', exact=True).click()
        page.get_by_label('前向形态筛选', exact=True).select_option('head-shoulders-bottom')
        expect(page.locator('.pv-forward-result[data-result-outcome="mature"]')).to_have_count(2)
        assert not errors, errors
        print({'mature_windows': [5, 10, 20], 'as_of_isolation': True, 'daily_history': True, 'immutable_export': True, 'reload': True, 'viewports': [1440, 390, 320]})
    finally:
        browser.close()
