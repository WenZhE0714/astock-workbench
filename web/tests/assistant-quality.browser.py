import argparse
from pathlib import Path
from playwright.sync_api import sync_playwright, expect

parser = argparse.ArgumentParser()
parser.add_argument('--url', required=True)
parser.add_argument('--output', default='/private/tmp/astock-assistant-quality-qa')
args = parser.parse_args()
out = Path(args.output)
out.mkdir(exist_ok=True)

answer = '''> 数据时效：行情与日K截至2026-10-20。

## 当前判断

**短线偏弱，先等收盘确认。** 现价100低于MA20 105，距该位5%。

## 关键依据

- 现价高于MA5 99，短线已有修复，不再等待首次收复。
- MA20仍未收复；盘中触及不代表完整日K确认。

## 改变判断的条件

1. 完整日K收盘守住 `MA20 105`，再复核结构。
2. 20日高点120超出今日涨停110，只作跨交易日结构位。

## 反证与缺口

若收盘不能守住近端均线，修复判断需要重审。

<img src=x onerror="window.assistantInjected=true">
[unsafe](javascript:alert(1))
'''
turn = {'question': '未持有，观察一周，现在适合介入吗？', 'answer': answer,
        'facts_at': '2026-10-20T15:30:00+08:00', 'facts_hash': 'sha256:fixture',
        'agents': [{'role': 'technical', 'label': '技术与量价', 'status': 'ok', 'analysis': '**近端修复**，现价已在MA5上方。'},
                   {'role': 'capital-sector', 'label': '资金与板块', 'status': 'failed'}]}

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    try:
        page = browser.new_page(viewport={'width': 1440, 'height': 1050})
        errors, questions = [], []
        page.on('pageerror', lambda error: errors.append(str(error)))
        page.route('**/api/assistant/context*', lambda r: r.fulfill(json={
            'symbol': 'sh600519', 'name': '测试股票', 'facts': {
                'quote': {'price': 100, 'percent': 1},
                'technical': {'bias': '看跌', 'data_date': '2026-10-20'},
                'warnings': ['资金增量缺失']}, 'history': [turn], 'alerts': []}))
        page.route('**/api/assistant/alerts*', lambda r: r.fulfill(json={'alerts': []}))

        def chat(route):
            if route.request.method == 'POST':
                questions.append(route.request.post_data_json)
                route.fulfill(status=202, json={'job_id': 'quality-test', 'status': 'queued'})
            else:
                route.fulfill(json={'job_id': 'quality-test', 'status': 'completed', 'symbol': 'sh600519',
                                    'question': questions[-1]['question'],
                                    'response': {'answer': '## 当前判断\n\n综合回答未完成，仅保留事实。',
                                                 'fallback': True, 'facts_at': turn['facts_at'], 'agents': turn['agents']}})
        page.route('**/api/assistant/chat*', chat)
        page.goto(args.url + '/?symbol=600519', wait_until='networkidle')
        page.get_by_role('button', name='打开 AI 研究助手', exact=True).click()
        panel = page.get_by_role('dialog', name='AI 研究助手', exact=True)
        expect(panel.locator('.assistant-answer-body h2').first).to_have_text('当前判断')
        expect(panel.locator('.assistant-answer-body strong').first).to_contain_text('短线偏弱')
        expect(panel.locator('.assistant-answer-body code').first).to_have_text('MA20 105')
        assert panel.locator('img, script, a[href^="javascript:"]').count() == 0
        assert page.evaluate('window.assistantInjected === undefined')
        panel.locator('summary').click()
        expect(panel.locator('.assistant-agent-details')).to_contain_text('近端修复')
        expect(panel.locator('.assistant-agent-details')).to_contain_text('不计入支持或反对证据')
        for width in [1440, 390, 320]:
            page.set_viewport_size({'width': width, 'height': 1050 if width > 600 else 844})
            assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'), width
            assert panel.evaluate('(e) => e.scrollWidth <= e.clientWidth'), width
            assert panel.locator('.assistant-panel-footer').bounding_box()['y'] < page.viewport_size['height']
            panel.locator('.assistant-thread').scroll_into_view_if_needed()
            page.screenshot(path=str(out / f'assistant-{width}.png'))
        question = '当前判断最强的反证是什么？'
        panel.get_by_role('button', name=question, exact=True).click()
        expect(panel.get_by_role('textbox', name='询问 AI 研究助手')).to_have_value(question)
        panel.get_by_role('button', name='发送问题', exact=True).click()
        expect(panel.locator('.assistant-fallback')).to_contain_text('仅展示确定性数据')
        assert questions[-1]['question'] == question
        assert questions[-1]['symbol'] == 'sh600519'
        assert not errors, errors
        print({'markdown': True, 'untrusted_content': True, 'role_details': True,
               'follow_up': True, 'fallback': True, 'desktop_mobile': True})
    finally:
        browser.close()
