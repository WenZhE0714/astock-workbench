import argparse
import json
from pathlib import Path
from playwright.sync_api import sync_playwright, expect

parser=argparse.ArgumentParser()
parser.add_argument('--url',required=True)
parser.add_argument('--output',default='/private/tmp/astock-forward-view-qa')
args=parser.parse_args()
out=Path(args.output)
out.mkdir(exist_ok=True)
expect.set_options(timeout=30000)
with sync_playwright() as p:
    browser=p.chromium.launch(headless=True)
    try:
        page=browser.new_page(viewport={'width':1440,'height':1050},accept_downloads=True)
        errors=[]
        page.on('pageerror',lambda error:errors.append(str(error)))
        page.goto(args.url+'/?view=strategy',wait_until='networkidle')
        panel=page.get_by_role('region',name='已冻结的P008前瞻实验')
        expect(panel).to_contain_text('已冻结 · 等待首个观察日')
        expect(panel).to_contain_text('10%')
        expect(panel).to_contain_text('≥ 1.5 倍')
        expect(panel).to_contain_text('0 / 60')
        expect(panel).to_contain_text('0 / 10')
        expect(panel).to_contain_text('2026-10-20')
        expect(panel.locator('[data-forward-group="candidate"]')).to_contain_text('--')
        expect(page.locator('.strategy-stage').first).to_contain_text('P008参数已冻结')
        expect(page.get_by_role('region',name='研究候选参数与未入选原因')).to_contain_text('通过 0 / 3 组')
        panel.get_by_role('tab',name='离线证据',exact=True).click()
        expect(panel.locator('.vf-table tbody tr')).to_have_count(4)
        expect(panel.locator('.vf-table tbody tr').last).to_contain_text('通过')
        panel.get_by_role('tab',name='冻结配置',exact=True).click()
        expect(panel).to_contain_text('sh600000')
        with page.expect_download() as download:
            panel.get_by_role('link',name='导出前瞻摘要',exact=True).click()
        path=out/'summary.json'
        download.value.save_as(path)
        report=json.loads(path.read_text())
        assert report['verified'] and report['read_only'] and report['sealed_days']==0
        assert report['plan']['promotion_allowed'] is False
        assert report['groups'][1]['return_percent'] is None
        for width in [1440,900,390,320]:
            page.set_viewport_size({'width':width,'height':1050 if width>600 else 844})
            assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'),width
            panel.get_by_role('tab',name='前瞻进度',exact=True).click()
            panel.screenshot(path=str(out/f'forward-{width}.png'))
        assert not errors,errors
        print({'frozen_parameters':True,'separate_old_candidates':True,'verified_evidence':True,'zero_days_no_returns':True,'export':True,'mobile':True})
    finally:
        browser.close()
