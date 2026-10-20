import argparse
import json
from pathlib import Path
from playwright.sync_api import sync_playwright, expect

parser = argparse.ArgumentParser()
parser.add_argument('--url', required=True)
parser.add_argument('--output', default='/private/tmp/astock-replay-browser')
args = parser.parse_args()
out = Path(args.output)
out.mkdir(parents=True, exist_ok=True)
expect.set_options(timeout=45000)

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    try:
        page = browser.new_page(viewport={'width':1440,'height':1080}, accept_downloads=True)
        errors = []
        page.on('pageerror', lambda error: errors.append(str(error)))
        page.goto(args.url+'/?view=strategy&strategy=replay',wait_until='networkidle',timeout=60000)
        panel = page.get_by_role('region', name='冻结交易复盘')
        expect(panel).to_be_visible()
        panel.get_by_label('复盘来源',exact=True).select_option('historical')
        expect(panel.locator('.rp-ledger tbody tr')).to_have_count(8)
        expect(panel.locator('.rp-account-summary')).to_contain_text('2026-10-08')
        panel.locator('.rp-ledger').get_by_role('button',name='通富微电',exact=True).first.click()
        expect(panel.get_by_label('所选交易详情')).to_contain_text('通富微电')
        expect(panel.locator('.rp-chart-heading')).to_contain_text('通富微电')
        host = panel.locator('.rp-daily-chart .replay-chart-canvas')
        page.wait_for_function("()=>{const host=document.querySelector('.rp-daily-chart .replay-chart-canvas');const r=host?.__vueParentComponent?.proxy.engine?.chart.timeScale().getVisibleLogicalRange();return r&&r.to-r.from<60;}")
        point = host.evaluate("el=>{const vm=el.__vueParentComponent.proxy;for(const m of vm.markers){const x=vm.engine.chart.timeScale().timeToCoordinate(m.time);if(x>10&&x<el.clientWidth-70)return {x,y:100,key:m.tradeKey};}throw Error('No visible trade marker');}")
        panel.get_by_role('button',name='取消选择',exact=True).click()
        host.click(position={'x':point['x'],'y':point['y']})
        expect(panel.get_by_label('所选交易详情')).to_be_visible()
        assert panel.locator('tr[data-selected-trade="true"]').count() == 1
        host.hover(position={'x':point['x'],'y':120})
        expect(panel.locator('.rp-ohlc')).to_contain_text('选中价位')
        page.screenshot(path=str(out/'selected-trade.png'),full_page=True)
        panel.get_by_role('tab', name='量比 1.5', exact=True).click()
        expect(panel.locator('.rp-ledger tbody tr')).to_have_count(2)
        expect(panel.get_by_label('所选交易详情')).to_contain_text('本组未成交')
        panel.get_by_role('tab', name='原规则', exact=True).click()
        with page.expect_response(lambda r:'/api/replay/day?' in r.url) as replay_response:
            panel.get_by_role('tab',name='逐日回放',exact=True).click()
        first = replay_response.value.json()
        assert first['date'] == '2026-07-16'
        assert all(b['time'] <= first['date'] for b in first['bars'] + first['weekly'] + first['benchmark'])
        assert all(not r['trades'] and not r.get('open_positions') for r in first['runs'])
        expect(panel.locator('.rp-ledger tbody tr')).to_have_count(0)
        expect(panel.get_by_role('button',name='创建价位提醒',exact=True)).to_be_disabled()
        expect(page.locator('.watchlist-market')).to_have_count(0)
        expect(page.get_by_role('button',name='打开 AI 研究助手',exact=True)).not_to_be_visible()
        with page.expect_response(lambda r:'/api/replay/day?' in r.url) as entry_response:
            panel.get_by_label('封存日期',exact=True).select_option('5')
        entry = entry_response.value.json()
        assert entry['date'] == '2026-07-23'
        assert len(entry['runs'][0].get('open_positions',[])) == 1
        assert not entry['runs'][0]['trades']
        expect(panel.locator('.rp-ledger tbody tr')).to_have_count(1)
        expect(panel.locator('.rp-ledger tbody')).to_contain_text('持仓')
        panel.get_by_role('button',name='后一日',exact=True).click()
        expect(panel.locator('.rp-account-summary')).to_contain_text('2026-07-24')
        expect(panel.locator('.rp-ledger tbody')).to_contain_text('已平仓')
        panel.get_by_role('button',name='播放',exact=True).click()
        expect(panel.get_by_role('button',name='暂停',exact=True)).to_be_visible()
        panel.get_by_role('button',name='暂停',exact=True).click()
        stopped = panel.get_by_label('封存日期',exact=True).input_value()
        page.wait_for_timeout(2000)
        assert panel.get_by_label('封存日期',exact=True).input_value() == stopped
        panel.get_by_label('回放日期',exact=True).evaluate("el=>{el.value='0';el.dispatchEvent(new Event('input',{bubbles:true}));el.value='5';el.dispatchEvent(new Event('input',{bubbles:true}));}")
        expect(panel.locator('.rp-account-summary')).to_contain_text('2026-07-23')
        expect(panel.locator('.rp-ledger tbody')).to_contain_text('持仓')
        panel.get_by_role('tab',name='复盘',exact=True).click()
        expect(panel.locator('.rp-account-summary')).to_contain_text('2026-10-08')
        panel.get_by_role('button',name='布局设置',exact=True).click()
        panel.get_by_label('周线',exact=True).uncheck()
        panel.get_by_label('布局名称',exact=True).fill('复盘核对')
        panel.get_by_role('button',name='保存布局',exact=True).click()
        preset_id = panel.get_by_label('工作区布局',exact=True).input_value()
        panel.get_by_label('股票筛选',exact=True).select_option('closed')
        panel.get_by_label('筛选名称',exact=True).fill('已有平仓')
        panel.get_by_role('button',name='保存筛选',exact=True).click()
        page.reload(wait_until='networkidle',timeout=60000)
        expect(panel.get_by_label('工作区布局',exact=True)).to_have_value(preset_id)
        panel.get_by_label('复盘来源',exact=True).select_option('historical')
        expect(panel.locator('.rp-ledger tbody tr')).to_have_count(8)
        panel.get_by_label('已保存筛选',exact=True).select_option(label='已有平仓')
        expect(panel.get_by_label('股票筛选',exact=True)).to_have_value('closed')
        panel.get_by_label('股票筛选',exact=True).select_option('all')
        if panel.get_by_role('button',name='加入复盘自选',exact=True).is_enabled():
            panel.get_by_role('button',name='加入复盘自选',exact=True).click()
            expect(panel.locator('.rp-notice')).to_contain_text('已加入自选')
        panel.get_by_label('仅自选',exact=True).check()
        expect(panel.locator('.rp-symbol-list button')).to_have_count(1)
        panel.get_by_label('仅自选',exact=True).uncheck()
        panel.get_by_role('button',name='创建价位提醒',exact=True).click()
        dialog = page.get_by_role('dialog',name='后台价位提醒草案')
        expect(dialog).to_be_visible()
        dialog.get_by_label('后台提醒价位',exact=True).fill('999999')
        before = page.request.get(args.url+'/api/price-alerts').json()['items']
        dialog.get_by_role('button',name='确认并启用后台提醒',exact=True).click()
        expect(dialog).not_to_be_visible()
        expect(panel.get_by_label('后台价位提醒')).to_contain_text('后台提醒已启用')
        alerts = page.request.get(args.url+'/api/price-alerts').json()['items']
        created = next(a for a in alerts if a['id'] not in {old['id'] for old in before})
        assert created['rule']['source']['id']=='historical' and created['rule']['price']==999999
        alert_row = panel.locator('[data-alert-id="'+created['id']+'"]')
        alert_row.get_by_role('button',name='暂停提醒',exact=True).click()
        expect(alert_row.locator('.pa-phase')).to_have_text('已暂停')
        alert_row.get_by_role('button',name='删除后台提醒',exact=True).click()
        expect(alert_row).to_have_count(0)
        panel.get_by_label('工作区布局',exact=True).select_option('review')
        with page.expect_download() as download:
            panel.get_by_role('button',name='导出当前快照',exact=True).click()
        download.value.save_as(out/'snapshot.json')
        exported = json.loads((out/'snapshot.json').read_text())
        assert exported['date'] == '2026-10-08' and exported['source']['kind'] == 'historical'
        canvas_colors = page.locator('.rp-daily-chart canvas').first.evaluate("c=>{const p=c.getContext('2d').getImageData(0,0,c.width,c.height).data; const colors=new Set();for(let i=0;i<p.length;i+=64)colors.add(p[i]+','+p[i+1]+','+p[i+2]);return colors.size}")
        assert canvas_colors > 10, canvas_colors
        for width in [1440,900,390,320]:
            page.set_viewport_size({'width':width,'height':1080 if width>600 else 844})
            page.wait_for_timeout(350)
            assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'), width
            assert panel.get_by_label('复盘来源',exact=True).bounding_box()['width'] > 120, width
            for selector in ['.rp-toolbar','.rp-chart-heading','.rp-universe']:
                assert panel.locator(selector).evaluate('el=>el.scrollWidth <= el.clientWidth+1'), (width,selector)
            page.screenshot(path=str(out/f'replay-{width}.png'),full_page=True)
        page.route('**/api/replay/day?**',lambda route:route.fulfill(status=409,content_type='application/json',body=json.dumps({'error':'封存归档校验失败'})))
        panel.get_by_role('button',name='刷新归档',exact=True).click()
        expect(panel.get_by_role('alert')).to_contain_text('封存归档校验失败')
        expect(panel.locator('.rp-daily-chart')).to_have_count(0)
        assert not errors, errors
        print({'trade_linking':True,'four_groups':True,'replay_cutoff':True,'play_pause':True,'rapid_seek':True,'saved_layout':True,'saved_filters':True,'watchlist':True,'explicit_alert_confirmation':True,'export':True,'canvas_colors':canvas_colors,'mobile':True,'error_hides_stale_data':True})
    finally:
        browser.close()
