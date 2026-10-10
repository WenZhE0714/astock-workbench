import argparse
import json
from datetime import datetime, timezone
from pathlib import Path

from playwright.sync_api import expect, sync_playwright


parser = argparse.ArgumentParser()
parser.add_argument('--url', default='http://127.0.0.1:8799')
parser.add_argument('--output', default='/private/tmp/astock-decision-check')
args = parser.parse_args()
output = Path(args.output)
output.mkdir(exist_ok=True)
errors = []

with sync_playwright() as playwright:
    browser = playwright.chromium.launch(headless=True)
    try:
        page = browser.new_page(viewport={'width': 1440, 'height': 1100})
        page.set_default_timeout(20000)
        page.on('pageerror', lambda error: errors.append(str(error)))
        page.clock.install(time=datetime(2026, 9, 18, 8, 0, tzinfo=timezone.utc))
        page.add_init_script('''window.deliveredNotifications = [];
          class TestNotification {
            static permission = 'default';
            static async requestPermission() { this.permission = 'granted'; return 'granted'; }
            constructor(title, options) { window.deliveredNotifications.push({title, ...options}); }
            close() {}
          }
          window.Notification = TestNotification;''')
        page.goto(args.url + '/?view=market&chart=daily', wait_until='networkidle')
        expect(page.get_by_role('heading', name='决策摘要', exact=True)).to_be_visible()
        page.locator('.position-preview > summary').click()
        page.get_by_label('试算账户权益', exact=True).fill('100000')
        page.get_by_label('试算可用现金', exact=True).fill('50000')
        invalid = page.locator('.position-preview input:invalid').evaluate_all('items => items.map(input => ({name:input.getAttribute("aria-label"),message:input.validationMessage}))')
        assert not invalid, invalid
        with page.expect_response(lambda response: '/api/position-preview' in response.url) as calculation:
            page.get_by_role('button', name='计算仓位', exact=True).click()
        result = calculation.value.json()
        assert calculation.value.status == 200 and result['shares'] > 0, result
        assert result['cash_required'] <= 50000 and result['total_risk'] <= 1000, result
        expect(page.locator('.risk-result')).to_be_visible()
        page.get_by_label('本股风险预算', exact=True).fill('0.5')
        expect(page.locator('.risk-result')).to_have_count(0)
        page.get_by_role('button', name='计算仓位', exact=True).click()
        expect(page.locator('.risk-result')).to_be_visible()
        page.get_by_label('本股风险预算', exact=True).fill('1')
        page.get_by_role('button', name='计算仓位', exact=True).click()
        expect(page.locator('.risk-result')).to_be_visible()
        for width in [1440, 390, 320]:
            page.set_viewport_size({'width': width, 'height': 1100 if width > 600 else 844})
            page.locator('.plan-decision').scroll_into_view_if_needed()
            assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'), width
            overflow = page.locator('.plan-decision :is(input,button,strong,dd)').evaluate_all('items => items.filter(el => el.getClientRects().length && el.clientWidth && el.scrollWidth > el.clientWidth + 2).map(el => el.textContent)')
            assert not overflow, overflow
            page.screenshot(path=str(output / f'decision-{width}.png'), full_page=True)
        page.set_viewport_size({'width': 1440, 'height': 1100})
        with page.expect_response(lambda response: response.request.method == 'POST' and '/api/trade-plan-monitors' in response.url):
            page.get_by_role('button', name='保存并启用监控', exact=True).click()
        expect(page.locator('.chart-plan-notice').filter(has_text='后台监控已启用')).to_be_visible()
        page.get_by_role('button', name='监控中心', exact=False).first.click()
        page.get_by_role('tab', name='提醒记录', exact=True).click()
        expect(page.get_by_role('heading', name='提醒记录', exact=True)).to_be_visible()
        expect(page.locator('.trade-playbook')).to_have_count(0)
        page.get_by_label('桌面提醒', exact=True).check()
        expect(page.get_by_label('桌面提醒', exact=True)).to_be_checked()
        assert page.evaluate('window.deliveredNotifications.length') == 0
        monitors = page.request.get(args.url + '/api/trade-plan-monitors').json()
        state = monitors['items'][0]
        event = {'id': 'browser-zone-entry', 'sequence': state['sequence'] + 1, 'kind': 'zone_entered', 'message': '测试计划进入观察区间', 'notify': True,
                 'observed_at': page.evaluate('new Date(Date.now() + 1000).toISOString()')}
        state['events'].append(event)
        state['sequence'] += 1
        page.route('**/api/trade-plan-monitors', lambda route: route.fulfill(json=monitors))
        page.get_by_role('button', name='刷新监控状态', exact=True).click()
        expect(page.locator('.local-alert-list')).to_contain_text('测试计划进入观察区间')
        page.wait_for_function('window.deliveredNotifications.length === 1')
        page.get_by_role('button', name='刷新监控状态', exact=True).click()
        page.wait_for_load_state('networkidle')
        assert page.evaluate('window.deliveredNotifications.length') == 1
        page.get_by_label('处理提醒 browser-zone-entry', exact=True).click()
        page.get_by_role('button', name='全部', exact=True).last.click()
        expect(page.locator('.local-alert-list')).to_contain_text('已处理')
        for width in [1440, 390, 320]:
            page.set_viewport_size({'width': width, 'height': 1100 if width > 600 else 844})
            assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'), width
            page.screenshot(path=str(output / f'notifications-{width}.png'), full_page=True)
        page.reload(wait_until='networkidle')
        assert page.evaluate('window.deliveredNotifications.length') == 0
        saved = page.evaluate('JSON.parse(localStorage.getItem("astock.local-alerts.v1"))')
        assert any(item['id'] == event['id'] and item['handled'] for item in saved['records'])
        print(json.dumps({'risk': result, 'notification_deduplication': True, 'notification_persistence': True}, ensure_ascii=False))
        assert not errors, errors
    finally:
        browser.close()
