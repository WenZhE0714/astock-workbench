import argparse
from pathlib import Path
from playwright.sync_api import sync_playwright, expect

parser=argparse.ArgumentParser()
parser.add_argument('--url',required=True)
parser.add_argument('--output',default='/private/tmp/astock-candidate-diagnostics')
args=parser.parse_args()
output=Path(args.output)
output.mkdir(exist_ok=True)
expect.set_options(timeout=30000)
with sync_playwright() as p:
    browser=p.chromium.launch(headless=True)
    try:
        page=browser.new_page(viewport={'width':1440,'height':1000})
        errors=[]
        page.on('pageerror',lambda error:errors.append(str(error)))
        page.goto(args.url+'/?view=strategy',wait_until='networkidle')
        panel=page.get_by_role('region',name='研究候选参数与未入选原因')
        expect(panel).to_be_visible()
        expect(page.get_by_role('button',name='刷新候选',exact=True)).to_be_enabled()
        page.get_by_role('button',name='刷新候选',exact=True).click()
        expect(page.get_by_role('button',name='刷新候选',exact=True)).to_be_enabled()
        expect(panel).to_contain_text('通过 0 / 3 组')
        expect(page.locator('.candidate-command-strip')).to_contain_text('未锁定：本轮 3 组候选均未通过滚动验证')
        expect(page.locator('.candidate-metrics strong').first).to_have_text('--')
        expect(page.get_by_role('button',name='批准进入观察',exact=True)).to_have_count(0)
        expect(panel).to_contain_text('正收益窗口 50% 低于门槛 67%')
        expect(panel.locator('.cd-parameters')).to_contain_text('MA')
        panel.get_by_role('button',name='查看候选 P008 参数',exact=True).click()
        expect(panel).to_contain_text('最差验证回撤 18.29% 超过门槛 15.00%')
        panel.get_by_role('button',name='查看候选 P003 参数',exact=True).click()
        expect(panel).to_contain_text('F02验证失败：数据不足')
        expect(panel.locator('.cd-table tbody').first.locator('tr').last).to_contain_text('--')
        for width in [1440,900,390,320]:
            page.set_viewport_size({'width':width,'height':1000 if width>600 else 844})
            assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'),width
            page.screenshot(path=str(output/f'candidate-{width}.png'),full_page=True)
        assert not errors,errors
        print({'rejected_parameters_visible':True,'gate_reasons':True,'no_false_lock':True,'incomplete_metrics_missing':True,'mobile':True})
    finally:
        browser.close()
