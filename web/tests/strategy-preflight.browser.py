import argparse
from pathlib import Path
from playwright.sync_api import sync_playwright, expect, Error

parser = argparse.ArgumentParser()
parser.add_argument('--url', required=True)
parser.add_argument('--output', default='/private/tmp/astock-preflight-qa')
args = parser.parse_args()
out = Path(args.output)
out.mkdir(exist_ok=True)
expect.set_options(timeout=30000)

stocks = [{'symbol': 'sh600584', 'name': '长电科技', 'kind': 'stock'},
          {'symbol': 'sz002837', 'name': '英维克', 'kind': 'stock'},
          {'symbol': 'sh688825', 'name': '长鑫科技', 'kind': 'stock'}]
names = {item['symbol']: item['name'] for item in stocks}
with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    try:
        page = browser.new_page(viewport={'width': 1440, 'height': 1050})
        errors, checks, runs, held = [], [], [], []
        mode = 'mixed'
        page.on('pageerror', lambda error: errors.append(str(error)))
        def report(body):
            items = []
            for symbol in body['symbols']:
                ready = mode != 'all' and (symbol != 'sh688825' or mode == 'benchmark')
                items.append({'symbol': symbol, 'name': names[symbol], 'ready': ready, 'bars': 700 if ready else 55,
                              'minimum_bars': body['slow_ma'] + 2, 'required_start': '2023-04-22',
                              'first_date': '2023-04-24' if ready else '2026-07-27', 'last_date': '2026-10-19',
                              'detail': '' if ready else '历史区间不足，当前策略至少需要62根日K'})
            available = sum(item['ready'] for item in items)
            return {'items': items, 'available': available, 'unavailable': len(items) - available,
                    'ready': available == len(items), 'start': body['start'], 'end': body['end'],
                    'checked_at': '2026-10-20T16:00:00+08:00',
                    'benchmark': {'ready': mode != 'benchmark', 'detail': '基准数据源不可用' if mode == 'benchmark' else ''}}
        def inspect(route):
            checks.append(route.request.post_data_json)
            if mode == 'hold':
                held.append(route)
            elif mode == 'error':
                route.fulfill(status=408, json={'error': '数据预检未完成: 检查超时'})
            else:
                route.fulfill(json=report(checks[-1]))
        def backtest(route):
            if route.request.method == 'POST':
                runs.append(route.request.post_data_json)
                route.fulfill(status=400, json={'error': '回测请求已验证'})
            else:
                route.continue_()
        page.route('**/api/watchlist*', lambda route: route.fulfill(json={'groups': [{'name': '默认', 'items': stocks}]}))
        page.route('**/api/strategy/preflight', inspect)
        page.route('**/api/strategy/backtests*', backtest)
        page.goto(args.url + '/?view=strategy', wait_until='networkidle')
        config = page.locator('.strategy-config')
        config.get_by_role('button', name='当前自选组', exact=True).click()
        run = config.get_by_role('button', name='运行回测', exact=True)
        panel = config.get_by_role('region', name='回测数据预检')
        run.click()
        expect(panel).to_contain_text('股票可用 2 / 3')
        expect(run).to_be_enabled()
        assert len(checks) == 1 and not runs
        expect(config.get_by_role('checkbox', name='本次回测包含 长鑫科技 688825', exact=True)).to_be_checked()
        expect(panel.locator('[data-preflight-symbol]')).to_have_count(3)
        for width in [1440, 390, 320]:
            page.set_viewport_size({'width': width, 'height': 1050 if width > 600 else 844})
            assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'), width
            panel.screenshot(path=str(out / f'preflight-{width}.png'))
        panel.get_by_role('button', name='本次排除不可用股票', exact=True).click()
        expect(config.locator('.strategy-pool input:checked')).to_have_count(2)
        expect(panel.locator('.sp-summary')).to_have_count(0)
        run.click()
        expect(config).to_contain_text('回测请求已验证')
        assert len(checks) == 2 and len(runs) == 1
        assert runs[0]['symbols'] == ['sh600584', 'sz002837']
        slow = config.get_by_label('慢均线', exact=True)
        slow.fill('80')
        expect(panel.locator('.sp-summary')).to_have_count(0)
        mode = 'hold'
        panel.get_by_role('button', name='检查回测数据', exact=True).click()
        expect(panel).to_have_attribute('aria-busy', 'true')
        slow.fill('90')
        expect(panel).to_have_attribute('aria-busy', 'false')
        for route in held:
            try:
                route.fulfill(json=report(route.request.post_data_json))
            except Error:
                pass
        held.clear()
        expect(panel.locator('.sp-summary')).to_have_count(0)
        assert len(runs) == 1
        panel.get_by_role('button', name='检查回测数据', exact=True).click()
        expect(panel).to_have_attribute('aria-busy', 'true')
        panel.get_by_role('button', name='取消数据预检', exact=True).click()
        expect(panel).to_contain_text('预检已取消')
        for route in held:
            try:
                route.abort()
            except Error:
                pass
        held.clear()
        mode = 'error'
        run.click()
        expect(panel.get_by_role('alert')).to_contain_text('检查超时')
        expect(run).to_be_enabled()
        assert len(runs) == 1
        mode = 'benchmark'
        panel.get_by_role('button', name='检查回测数据', exact=True).click()
        expect(panel).to_contain_text('基准不可用，收益对照受限')
        assert checks[-1]['slow_ma'] == 90
        mode = 'all'
        panel.get_by_role('button', name='检查回测数据', exact=True).click()
        expect(panel).to_contain_text('股票可用 0 / 2')
        expect(panel.get_by_role('button', name='本次排除不可用股票', exact=True)).to_have_count(0)
        assert not errors, errors
        print({'auto_preflight': True, 'all_failures_visible': True, 'explicit_exclusion': True,
               'changed_inputs_invalidate': True, 'late_response_ignored': True, 'cancellation': True,
               'timeout_blocks_run': True, 'benchmark_warning': True, 'mobile': True})
    finally:
        browser.close()
