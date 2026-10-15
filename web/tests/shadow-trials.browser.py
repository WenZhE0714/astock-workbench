import argparse
from pathlib import Path

from playwright.sync_api import expect, sync_playwright

parser = argparse.ArgumentParser()
parser.add_argument('--url', required=True)
parser.add_argument('--output', default='/private/tmp/astock-shadow-trials-browser')
args = parser.parse_args()
output = Path(args.output)
output.mkdir(parents=True, exist_ok=True)
expect.set_options(timeout=30000)

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    try:
        page = browser.new_page(viewport={'width': 1440, 'height': 1000})
        errors = []
        page.on('pageerror', lambda error: errors.append(str(error)))
        page.goto(args.url + '/?view=realtime&realtime=shadow', wait_until='networkidle')
        with page.expect_response(lambda r: '/api/strategy/shadow?profile=trial-balanced' in r.url):
            page.get_by_role('button', name='优化对照', exact=True).click()
        expect(page.locator('.shadow-profile-empty')).to_have_count(0)
        expect(page.locator('.shadow-profile-card')).to_have_count(6)
        tabs = page.get_by_role('tablist', name='影子账户切换')
        for name, profile_id in [('均衡对照', 'trial-balanced'), ('均衡不加仓', 'trial-balanced-no-add'), ('均衡延伸上限', 'trial-balanced-bounded'), ('抓妖对照', 'trial-monster'), ('抓妖不加仓', 'trial-monster-no-add'), ('抓妖盘中入场', 'trial-monster-intraday')]:
            tab = tabs.get_by_role('tab').filter(has=page.locator('strong', has_text=name))
            if tab.get_attribute('aria-selected') != 'true':
                with page.expect_response(lambda r: r.url.endswith('/api/strategy/shadow?profile=' + profile_id)) as loaded:
                    tab.click()
                payload = loaded.value.json()
                assert payload['account']['id'] == profile_id and payload['report'] is not None
            expect(page.locator('.shadow-profile-empty')).to_have_count(0)
            expect(page.locator('.shadow-profile-note').first).to_contain_text(name)
            expect(page.locator('.shadow-profile-control')).to_contain_text('账本校验通过')
        expect(page.locator('.shadow-profile-control')).to_contain_text('较同起点基线')
        for width in [1440, 390, 320]:
            page.set_viewport_size({'width': width, 'height': 1000 if width > 600 else 844})
            assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'), width
            clipped = page.locator('.shadow-trial-grid :is(b, strong, small)').evaluate_all('items => items.filter(el => el.getClientRects().length && el.clientWidth && el.scrollWidth > el.clientWidth + 2).map(el => el.textContent)')
            assert not clipped, clipped
            page.screenshot(path=str(output / f'trials-{width}.png'), full_page=True)
        page.get_by_role('button', name='原账户', exact=True).click()
        expect(page.get_by_role('tab', name='均衡型', exact=False)).to_be_visible()
        assert not errors, errors
        print({'paired_profiles': 6, 'group_navigation': True, 'audit_visible': True, 'viewports': [1440, 390, 320]})
    finally:
        browser.close()
