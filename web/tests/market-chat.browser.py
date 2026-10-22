import argparse
from pathlib import Path
from playwright.sync_api import sync_playwright, expect

parser = argparse.ArgumentParser()
parser.add_argument('--url', required=True)
parser.add_argument('--output', default='/private/tmp/astock-market-chat-browser')
args = parser.parse_args()
out = Path(args.output)
out.mkdir(exist_ok=True)
assessment = {'id': '1792570000000000000-abcdef123456', 'anchor_symbol': 'sh000001',
              'base_date': '2026-10-21', 'base_close': 3713.03, 'neutral_band_percent': 1,
              'evaluable': True, 'predictions': [{'trading_days': 5, 'direction': 'range'},
                                               {'trading_days': 20, 'direction': 'bearish'}]}
question = '现在大盘趋势怎么样，后续看涨还是看跌'
answer = '## 当前状态\n\n三大指数分化。\n\n## 未来方向\n\n5个交易日震荡，20个交易日偏空。'
turn = {'question': question, 'answer': answer, 'assessment': assessment}

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    try:
        page = browser.new_page(viewport={'width': 1440, 'height': 1000})
        errors, requests, review_calls = [], [], []
        completed = False
        page.on('pageerror', lambda e: errors.append(str(e)))
        def context(route):
            market = 'symbol=market' in route.request.url
            data = {'symbol': 'market' if market else 'sh600519', 'name': '大盘 · 三大指数' if market else '贵州茅台',
                    'facts': {'quote': {'price': 100}, 'technical': {}}, 'history': [turn] if market and completed else [], 'alerts': []}
            if market:
                data['market_facts'] = {'indices': [{'quote': {'symbol': s, 'name': n, 'price': v, 'percent': -0.5}}
                                                   for s, n, v in [('sh000001', '上证指数', 3713.03), ('sz399001', '深证成指', 11000), ('sz399006', '创业板', 2500)]]}
            route.fulfill(json=data)
        def chat(route):
            if route.request.method == 'POST':
                requests.append(route.request.post_data_json)
                route.fulfill(status=202, json={'job_id': 'market-test', 'status': 'queued', 'symbol': 'market'})
            else:
                route.fulfill(json={'job_id': 'market-test', 'status': 'completed', 'symbol': 'market', 'question': question,
                                    'response': {'answer': answer, 'assessment': assessment, 'facts_at': '2026-10-21T20:00:00+08:00'}})
        def review(route):
            review_calls.append(route.request.url)
            route.fulfill(json={'outcomes': [{'trading_days': 5, 'status': 'waiting', 'observed_days': 2},
                                            {'trading_days': 20, 'status': 'waiting', 'observed_days': 2}]})
        page.route('**/api/assistant/context*', context)
        page.route('**/api/assistant/alerts*', lambda r: r.fulfill(json={'alerts': []}))
        page.route('**/api/assistant/chat*', chat)
        page.route('**/api/assistant/market-review*', review)
        page.goto(args.url + '/?symbol=600519', wait_until='networkidle')
        assert not errors, errors
        page.get_by_role('button', name='打开 AI 研究助手', exact=True).click()
        panel = page.get_by_role('dialog', name='AI 研究助手', exact=True)
        panel.get_by_role('textbox', name='询问 AI 研究助手').fill(question)
        panel.get_by_role('button', name='发送问题', exact=True).click()
        expect(panel.get_by_label('分析对象')).to_have_value('market')
        expect(panel.locator('.assistant-context-metrics')).to_contain_text('创业板')
        expect(panel.locator('.assistant-user')).to_contain_text(question)
        expect(panel.locator('.assistant-answer-body')).to_contain_text('5个交易日震荡，20个交易日偏空')
        expect(panel.locator('.assistant-answer-body h2').first).to_have_text('当前状态')
        region = panel.get_by_role('region', name='大盘判断复核')
        expect(region).to_contain_text('5交易日 · 震荡')
        expect(region).to_contain_text('20交易日 · 偏空')
        completed = True
        expect(panel.locator('.assistant-rule-actions')).to_have_count(0)
        expect(panel.locator('.assistant-alert-section')).to_have_count(0)
        region.get_by_role('button', name='复核后续走势').click()
        expect(region).to_contain_text('待到期 2/5日')
        expect(region).to_contain_text('待到期 2/20日')
        expect(region.get_by_role('link', name='下载原始证据')).to_have_attribute('href', '/api/assistant/market-review?id=' + assessment['id'] + '&evidence=1')
        for width in [1440, 390, 320]:
            page.set_viewport_size({'width': width, 'height': 1000 if width > 600 else 844})
            region.scroll_into_view_if_needed()
            assert panel.evaluate('(e) => e.scrollWidth <= e.clientWidth'), width
            assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'), width
            page.screenshot(path=str(out / f'market-chat-{width}.png'))
        panel.get_by_label('分析对象').select_option('security')
        expect(panel.locator('.assistant-context-head')).to_contain_text('贵州茅台')
        expect(panel.get_by_role('region', name='大盘判断复核')).to_have_count(0)
        assert len(requests) == 1 and requests[0]['scope'] == 'auto'
        assert len(review_calls) == 1
        assert not errors, errors
        print({'automatic_market_routing': True, 'three_indices': True, 'frozen_assessment': True,
               'pending_review': True, 'evidence_link': True, 'return_to_security': True, 'responsive': True})
    finally:
        browser.close()
