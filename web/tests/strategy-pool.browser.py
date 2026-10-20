import argparse
from pathlib import Path
from playwright.sync_api import sync_playwright, expect

parser = argparse.ArgumentParser()
parser.add_argument('--url', required=True)
parser.add_argument('--output', default='/private/tmp/astock-strategy-pool-qa')
args = parser.parse_args()
out = Path(args.output)
out.mkdir(exist_ok=True)
expect.set_options(timeout=30000)

stocks = [{'symbol': f'sh600{index:03d}', 'kind': 'stock', 'name': f'A股{index + 1}'} for index in range(20)]
etf = {'symbol': 'sh516350', 'kind': 'stock', 'name': '芯片ETF易方达'}
watchlist = {'groups': [{'name': '默认', 'items': stocks + [etf]}, {'name': '科技', 'items': stocks[:1]},
                         {'name': 'ETF', 'items': [etf]}]}
with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    try:
        page = browser.new_page(viewport={'width': 1440, 'height': 1050})
        errors = []
        writes = []
        submitted = []
        page.on('pageerror', lambda error: errors.append(str(error)))
        def watchlist_route(route):
            if route.request.method != 'GET':
                writes.append(route.request.method)
            route.fulfill(json=watchlist)
        def backtest_route(route):
            if route.request.method == 'POST':
                submitted.append(route.request.post_data_json)
                if len(submitted) == 1:
                    route.fulfill(status=502, json={'error': 'A股1 的历史日K不足，当前回测未完成',
                        'history_issue': {'symbol': stocks[0]['symbol'], 'name': 'A股1', 'detail': '有效日K不足：55 根，当前策略至少需要 62 根'}})
                else:
                    route.fulfill(status=400, json={'error': '受控测试请求已截获'})
            else:
                route.continue_()
        page.route('**/api/watchlist*', watchlist_route)
        page.route('**/api/strategy/backtests*', backtest_route)
        page.goto(args.url + '/?view=strategy', wait_until='networkidle')
        config = page.locator('.strategy-config')
        config.get_by_role('button', name='当前自选组', exact=True).click()
        group = config.get_by_label('回测自选分组', exact=True)
        expect(group).to_have_value('全部')
        group.select_option('默认')
        expect(config.locator('.strategy-pool > label')).to_have_count(20)
        expect(config.locator('.strategy-pool')).not_to_contain_text('芯片ETF')
        expect(config.locator('.strategy-excluded')).to_contain_text('已排除 1 个非 A 股标的')
        config.locator('.strategy-excluded summary').click()
        expect(config.locator('.strategy-excluded')).to_contain_text('芯片ETF易方达')
        run = config.get_by_role('button', name='运行回测', exact=True)
        expect(run).to_be_enabled()
        with page.expect_request(lambda request: '/api/strategy/backtests' in request.url and request.method == 'POST'):
            run.click()
        expect(config.get_by_role('alert')).to_contain_text('历史日K不足')
        assert len(submitted) == 1 and submitted[0]['symbols'] == [item['symbol'] for item in stocks]
        expect(config.locator('.strategy-pool input:checked')).to_have_count(20)
        expect(config.locator('.strategy-history-detail p')).not_to_be_visible()
        config.get_by_role('button', name='本次回测排除 A股1', exact=True).click()
        expect(config.locator('.strategy-pool input:checked')).to_have_count(19)
        expect(config.locator('.strategy-pool > label')).to_have_count(20)
        expect(config.get_by_role('alert')).to_have_count(0)
        with page.expect_request(lambda request: '/api/strategy/backtests' in request.url and request.method == 'POST'):
            run.click()
        expect(config).to_contain_text('受控测试请求已截获')
        assert len(submitted) == 2 and submitted[1]['symbols'] == [item['symbol'] for item in stocks[1:]]
        for width in [1440, 390, 320]:
            page.set_viewport_size({'width': width, 'height': 1050 if width > 600 else 844})
            assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'), width
            config.screenshot(path=str(out / f'pool-{width}.png'))
        group.select_option('ETF')
        expect(config.locator('.strategy-pool > label')).to_have_count(0)
        expect(run).to_be_disabled()
        expect(config).to_contain_text('当前范围没有可回测的 A 股')
        group.select_option('科技')
        expect(config.locator('.strategy-pool > label')).to_have_count(1)
        expect(run).to_be_enabled()
        config.locator('.strategy-pool input').uncheck()
        expect(run).to_be_disabled()
        expect(config).to_contain_text('尚未勾选本次回测股票')
        watchlist['groups'].append({'name': '超限', 'items': stocks + [{'symbol': 'sh600999', 'kind': 'stock', 'name': '额外股票'}]})
        page.reload(wait_until='networkidle')
        config.get_by_role('button', name='当前自选组', exact=True).click()
        group.select_option('超限')
        expect(config.locator('.strategy-pool > label')).to_have_count(21)
        expect(config.get_by_role('alert')).to_contain_text('当前股票池有 21 只 A 股，单次最多 20 只')
        expect(run).to_be_disabled()
        config.get_by_role('checkbox', name='本次回测包含 额外股票 600999', exact=True).uncheck()
        expect(run).to_be_enabled()
        expect(config.locator('.strategy-pool input:checked')).to_have_count(20)
        assert len(submitted) == 2 and not writes
        assert not errors, errors
        print({'twenty_stocks_plus_etf': True, 'exact_payload': True, 'group_switch': True,
               'etf_only_disabled': True, 'over_limit_blocked': True, 'explicit_failure_exclusion': True,
               'selection_reset_on_group_change': True, 'watchlist_unchanged': True, 'mobile': True})
    finally:
        browser.close()
