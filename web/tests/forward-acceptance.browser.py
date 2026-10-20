import argparse
import copy
from pathlib import Path
from playwright.sync_api import sync_playwright, expect

parser = argparse.ArgumentParser()
parser.add_argument('--url', required=True)
parser.add_argument('--output', default='/private/tmp/astock-forward-acceptance-qa')
args = parser.parse_args()
out = Path(args.output)
out.mkdir(exist_ok=True)
expect.set_options(timeout=30000)

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    try:
        page = browser.new_page(viewport={'width': 1440, 'height': 1050})
        errors = []
        page.on('pageerror', lambda error: errors.append(str(error)))
        base = page.request.get(args.url + '/api/strategy/volume-forward').json()
        report = {
            'checked_at': '2026-10-23T16:00:00+08:00', 'status': 'waiting',
            'detail': '等待首份真实日档', 'sealed_days': 0,
            'execution': {'status': 'waiting-first-day', 'detail': '尚无成交验收证据', 'transitions': 0,
                          'accounts': [{'account': name, 'buys': 0, 'sells': 0, 'deferred_orders': 0}
                                       for name in ['原规则', '量比1.5', '原规则双倍成本', '量比1.5双倍成本']]}}
        payload = copy.deepcopy(base)
        payload['acceptance'] = {'configured': True, 'bound': True, 'current': True, 'stale': False, 'report': report}
        page.route('**/api/strategy/volume-forward', lambda route: route.fulfill(json=payload))
        page.goto(args.url + '/?view=strategy&forward=acceptance', wait_until='networkidle')
        panel = page.get_by_role('region', name='已冻结的P008前瞻实验')
        summary = panel.get_by_label('自动验收摘要')
        detail = panel.get_by_label('自动验收详情')
        expect(panel.get_by_role('tab', name='自动验收', exact=True)).to_have_attribute('aria-selected', 'true')
        expect(summary).to_contain_text('等待首日验收')
        expect(detail.locator('[data-execution-account]')).to_have_count(4)
        expect(detail).to_contain_text('首次卖出 0 / 4组')

        def refresh():
            with page.expect_response('**/api/strategy/volume-forward'):
                panel.get_by_role('button', name='刷新前瞻状态').click()
            expect(panel).to_have_attribute('aria-busy', 'false')

        report.update(status='passed', first_date='2026-10-20', sealed_days=1, detail='首日四组账户验收通过')
        payload['sealed_days'] = 1
        report['execution'].update(status='waiting-next-day', detail='等待后续日档验证执行')
        refresh()
        expect(summary).to_contain_text('首日验收通过')
        expect(detail).to_contain_text('等待后续日档')

        payload['sealed_days'] = 4
        report['sealed_days'] = 4
        report['execution'].update(status='partial', detail='部分账户首次买入通过', transitions=3, verified_through='2026-10-23')
        report['execution']['accounts'][0].update(first_buy='2026-10-21', buys=1)
        refresh()
        expect(summary).to_contain_text('首次买入 1 / 4组')
        expect(summary).to_contain_text('首次卖出 0 / 4组')
        expect(detail).to_contain_text('部分账户首次买入已核验')
        expect(detail.locator('[data-execution-account="量比1.5"]')).to_contain_text('尚未发生')
        for width in [1440, 900, 390, 320]:
            page.set_viewport_size({'width': width, 'height': 1050 if width > 600 else 844})
            assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'), width
            buttons = panel.get_by_role('tab').all()
            boxes = [button.bounding_box() for button in buttons]
            for box in boxes:
                assert box['width'] > 0 and box['x'] >= 0 and box['x'] + box['width'] <= width + 1, (width, box)
            panel.screenshot(path=str(out / f'acceptance-{width}.png'))

        page.set_viewport_size({'width': 1440, 'height': 1050})
        for account in report['execution']['accounts']:
            account.update(first_buy='2026-10-21', buys=1)
        report['execution'].update(status='passed', detail='四组首次买入已核验')
        refresh()
        expect(summary).to_contain_text('首次买入 4 / 4组')
        expect(summary).to_contain_text('首次卖出 0 / 4组')
        expect(detail).to_contain_text('策略未晋级')

        report['execution']['accounts'][0].update(first_sell='2026-10-22', sells=1, deferred_orders=2)
        refresh()
        expect(summary).to_contain_text('首次卖出 1 / 4组')
        expect(detail.locator('[data-execution-account="原规则"]')).to_contain_text('2026-10-22')

        payload['acceptance'].update(current=False, stale=True)
        refresh()
        expect(summary).to_contain_text('验收状态待更新')
        expect(summary).not_to_contain_text('首日验收通过')
        expect(detail).to_contain_text('报告超过15分钟未更新')
        expect(detail).to_contain_text('最近记录 · 四组首次买入已核验')
        payload['acceptance']['stale'] = False
        payload['sealed_days'] = 5
        refresh()
        expect(summary).to_contain_text('等待验收追平日档')
        expect(detail).to_contain_text('验收尚未覆盖当前日档')

        report.update(status='error', detail='成交资金核对失败')
        report['execution'].update(status='error', detail='现金与封存账务不一致')
        refresh()
        expect(summary).to_contain_text('验收异常')
        expect(detail).to_contain_text('现金与封存账务不一致')
        panel.screenshot(path=str(out / 'acceptance-error.png'))

        payload['acceptance'] = {'configured': True, 'bound': False, 'current': False, 'error': '验收报告的实验指纹不匹配'}
        refresh()
        expect(detail.get_by_role('alert')).to_contain_text('实验指纹不匹配')
        expect(detail.locator('[data-execution-account]')).to_have_count(0)
        expect(summary).to_contain_text('首次买入 未记录')
        report.pop('execution')
        report.update(status='passed', detail='首日验收通过')
        payload['acceptance'] = {'configured': True, 'bound': True, 'current': False, 'report': report}
        refresh()
        expect(detail).to_contain_text('尚无成交验收记录')
        expect(summary).to_contain_text('首次卖出 未记录')
        payload['acceptance'] = {'configured': False}
        refresh()
        expect(detail).to_contain_text('尚未接入独立验收报告')

        page.route('**/api/native-notifications', lambda route: route.fulfill(json={
            'enabled': True, 'items': [{'id': 'forward-error', 'kind': 'forward-collection',
                'title': 'ASTOCK · 前瞻采集失败', 'body': '测试异常', 'at': '2026-10-23T16:00:00+08:00', 'status': 'submitted'}],
            'reviews': {'items': {}, 'symbols': {}}}))
        page.goto(args.url + '/?view=monitor&monitor=native', wait_until='networkidle')
        link = page.get_by_role('link', name='查看前瞻验收')
        expect(link).to_have_attribute('href', '/?view=strategy&forward=acceptance')
        link.click()
        expect(page.get_by_role('tab', name='自动验收', exact=True)).to_have_attribute('aria-selected', 'true')
        assert not errors, errors
        print({'acceptance_states': True, 'first_buys_and_sells_separate': True, 'stale_and_unbound': True,
               'legacy_report': True, 'notification_link': True, 'mobile_320_390': True, 'console_errors': errors})
    finally:
        browser.close()
