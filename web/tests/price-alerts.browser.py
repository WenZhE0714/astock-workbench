import argparse
import json
import time
from pathlib import Path
from playwright.sync_api import sync_playwright, expect

parser=argparse.ArgumentParser()
parser.add_argument('--url',required=True)
parser.add_argument('--output',default='/private/tmp/astock-price-alerts-qa')
args=parser.parse_args()
out=Path(args.output);out.mkdir(parents=True,exist_ok=True)
expect.set_options(timeout=30000)

with sync_playwright() as p:
    browser=p.chromium.launch(headless=True)
    context=browser.new_context(viewport={'width':1440,'height':1080})
    api=context.request
    errors=[]
    def new_page(path='/?view=monitor&monitor=prices'):
        page=context.new_page()
        page.on('pageerror',lambda e:errors.append(str(e)))
        page.goto(args.url+path,wait_until='networkidle',timeout=60000)
        return page
    def items():
        r=api.get(args.url+'/api/price-alerts');assert r.ok,r.text();return r.json()['items']
    def wait_trigger(id):
        deadline=time.monotonic()+20
        while time.monotonic()<deadline:
            item=next(a for a in items() if a['id']==id)
            if item.get('event') and item.get('notification',{}).get('status')=='submitted':return item
            time.sleep(.2)
        raise AssertionError('background trigger was not delivered')
    try:
        page=new_page()
        panel=page.get_by_role('region',name='后台价位提醒')
        expect(panel).to_contain_text('后台检查中')
        panel.get_by_role('button',name='新建提醒',exact=True).click()
        dialog=page.get_by_role('dialog',name='后台价位提醒草案')
        dialog.get_by_label('提醒股票',exact=True).fill('sh600110')
        dialog.get_by_label('提醒名称',exact=True).fill('后台检查样例')
        dialog.get_by_label('后台提醒价位',exact=True).fill('10')
        assert items()==[]
        dialog.get_by_role('button',name='确认并启用后台提醒',exact=True).click()
        expect(dialog).not_to_be_visible()
        expect(panel.locator('.pa-item')).to_have_count(1)
        first=items()[0];id=first['id']
        row=panel.locator('[data-alert-id="'+id+'"]')
        row.get_by_role('button',name='暂停提醒',exact=True).click()
        expect(row.locator('.pa-phase')).to_have_text('已暂停')
        api.post(args.url+'/test/quote?price=11')
        time.sleep(1.5)
        assert not items()[0].get('event')
        api.post(args.url+'/test/quote?price=9')
        row.get_by_role('button',name='启用提醒',exact=True).click()
        expect(row.locator('.pa-phase')).to_have_text('监控中')
        page.close()
        assert len(context.pages)==0
        api.post(args.url+'/test/quote?price=11')
        fired=wait_trigger(id)
        assert fired['event']['price']==11
        assert api.get(args.url+'/test/submissions').json()['count']==1
        page=new_page()
        panel=page.get_by_role('region',name='后台价位提醒')
        row=panel.locator('[data-alert-id="'+id+'"]')
        expect(row).to_contain_text('已触发')
        expect(row).to_contain_text('已提交系统')
        page.reload(wait_until='networkidle')
        assert api.get(args.url+'/test/submissions').json()['count']==1
        legacy={'id':'price-legacy-12345678','symbol':'sh600110','name':'旧本页提醒','direction':'above','price':99,'expires_on':'2026-10-27','enabled':True}
        page.evaluate('(a)=>localStorage.setItem("astock.replay-price-alerts.v1",JSON.stringify([a]))',legacy)
        page.reload(wait_until='networkidle')
        expect(panel.locator('.pa-legacy')).to_contain_text('旧本页提醒')
        panel.get_by_role('button',name='确认迁移',exact=True).click()
        assert len(items())==1
        dialog=page.get_by_role('dialog',name='后台价位提醒草案')
        dialog.get_by_role('button',name='确认并启用后台提醒',exact=True).click()
        expect(dialog).not_to_be_visible()
        expect(panel.locator('.pa-item')).to_have_count(2)
        assert page.evaluate('JSON.parse(localStorage.getItem("astock.replay-price-alerts.v1")).length')==0
        old=next(a for a in items() if a['rule']['name']=='旧本页提醒')
        panel.locator('[data-alert-id="'+old['id']+'"]').get_by_role('button',name='删除后台提醒',exact=True).click()
        expect(panel.locator('.pa-item')).to_have_count(1)
        for width in [1440,390,320]:
            page.set_viewport_size({'width':width,'height':1080 if width>600 else 844})
            assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'),width
            assert panel.evaluate('el=>el.scrollWidth<=el.clientWidth+1'),width
            page.screenshot(path=str(out/f'alerts-{width}.png'),full_page=True)
        page.close()
        page=new_page('/?view=strategy&strategy=replay')
        replay=page.get_by_role('region',name='冻结交易复盘')
        expect(replay.locator('.rp-account-summary')).to_contain_text('2026-10-08')
        expect(replay.get_by_role('region',name='后台价位提醒')).to_contain_text('后台检查中')
        replay.get_by_role('button',name='创建价位提醒',exact=True).click()
        dialog=page.get_by_role('dialog',name='后台价位提醒草案')
        expect(dialog).to_contain_text('历史演练 2026-10-08')
        dialog.get_by_label('后台提醒价位',exact=True).fill('999')
        dialog.get_by_role('button',name='确认并启用后台提醒',exact=True).click()
        expect(dialog).not_to_be_visible()
        source_alert=next(a for a in items() if a['rule'].get('source'))
        assert source_alert['rule']['source']['id']=='historical'
        replay.get_by_role('tab',name='逐日回放',exact=True).click()
        expect(replay.locator('.rp-account-summary')).to_contain_text('2026-07-16')
        expect(replay.get_by_role('region',name='后台价位提醒')).to_have_count(0)
        api.post(args.url+'/test/quote?price=1000')
        wait_trigger(source_alert['id'])
        expect(replay.locator('.rp-account-summary')).to_contain_text('2026-07-16')
        assert '1,000.00' not in replay.locator('.rp-ohlc').inner_text()
        assert api.get(args.url+'/test/submissions').json()['count']==2
        assert not errors,errors
        result={'explicit_confirmation':True,'pause_resume':True,'closed_page_background_trigger':True,'persistent_dedup':True,'legacy_confirmation':True,'delete':True,'snapshot_origin':True,'replay_ui_isolation':True,'mobile':True,'desktop_messages':'recording sender only'}
        (out/'result.json').write_text(json.dumps(result,ensure_ascii=False,indent=2))
        print(result)
    finally:
        try:api.post(args.url+'/test/stop')
        finally:browser.close()
