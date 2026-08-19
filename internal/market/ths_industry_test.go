package market

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

func TestParseTHSIndustryDetail(t *testing.T) {
	raw := `<div class="board-hq"><h3>银行<span>881155</span></h3>
	<span class="board-xj arr-rise">1332.93</span><p class="board-zdf">12.71&nbsp;&nbsp;0.96%</p></div>
	<div class="board-infos">
	<dl><dt>今开</dt><dd>1324.40</dd></dl><dl><dt>昨收</dt><dd>1320.22</dd></dl>
	<dl><dt>最低</dt><dd>1324.40</dd></dl><dl><dt>最高</dt><dd>1340.48</dd></dl>
	<dl><dt>成交量(万手)</dt><dd>3182.86</dd></dl><dl><dt>成交额(亿)</dt><dd>253.90</dd></dl>
	<dl><dt>板块涨幅</dt><dd class="c-rise">0.89%</dd></dl>
	<dl><dt>涨幅排名</dt><dd>3/90</dd></dl>
	<dl><dt>涨跌家数</dt><dd><span class="arr-rise-s">39</span><span class="arr-fall-s">0</span></dd></dl>
	<dl><dt>资金净流入(亿)</dt><dd>17.07</dd></dl>
	</div>
	<table><tbody><tr>
	<td>1</td><td><a>601998</a></td><td><a>中信银行</a></td><td>8.08</td><td>3.46</td>
	<td>0.27</td><td>0.00</td><td>0.10</td><td>3.73</td><td>3.84</td><td>3.33亿</td>
	</tr></tbody></table>`
	flow, leaders, err := ParseTHSIndustryDetail(raw, "881155")
	if err != nil {
		t.Fatal(err)
	}
	if flow.Code != "th881155" || flow.Name != "银行" || flow.Percent != 0.89 || flow.MainNet != 17.07e8 || flow.RiseCount != 39 || flow.FallCount != 0 {
		t.Fatalf("unexpected flow: %+v", flow)
	}
	if flow.Quote == nil || flow.Quote.Price != 1332.93 || flow.Quote.Delta != 12.71 || flow.Quote.Open != 1324.40 || flow.Quote.PreviousClose != 1320.22 || flow.Quote.High != 1340.48 || flow.Quote.Low != 1324.40 || flow.Quote.Volume != 3182.86 || flow.Quote.Amount != 253.90e8 || flow.ChangeRank != 3 || flow.UniverseSize != 90 {
		t.Fatalf("unexpected board quote: %+v", flow)
	}
	if !math.IsNaN(flow.MainRatio) || !math.IsNaN(flow.Turnover) {
		t.Fatalf("unavailable THS metrics should remain NaN: %+v", flow)
	}
	if len(leaders) != 1 || leaders[0].Symbol != "sh601998" || leaders[0].Name != "中信银行" || leaders[0].Amount != 3.33e8 || leaders[0].Speed != 0 || leaders[0].Turnover != 0.10 || leaders[0].VolumeRatio != 3.73 {
		t.Fatalf("unexpected leaders: %+v", leaders)
	}
}

func TestParseTHSIndustryDetailReturnsTopTenConstituents(t *testing.T) {
	var rows strings.Builder
	for index := 0; index < 12; index++ {
		fmt.Fprintf(&rows, `<tr><td>%d</td><td>60%04d</td><td>银行%d</td><td>8.08</td><td>3.46</td><td>0.27</td><td>0.10</td><td>0.20</td><td>1.50</td><td>3.84</td><td>3.33亿</td></tr>`, index+1, index, index+1)
	}
	raw := `<h3>银行<span>881155</span></h3><tbody>` + rows.String() + `</tbody>`
	_, leaders, err := ParseTHSIndustryDetail(raw, "881155")
	if err != nil {
		t.Fatal(err)
	}
	if len(leaders) != 10 || leaders[0].Name != "银行1" || leaders[9].Name != "银行10" {
		t.Fatalf("unexpected top-ten constituents: %+v", leaders)
	}
}

func TestParseTHSIndustryDetailRejectsUnknownCode(t *testing.T) {
	if _, _, err := ParseTHSIndustryDetail(`<h3>银行<span>881155</span></h3>`, "881999"); err == nil {
		t.Fatal("expected mismatched THS industry code to fail")
	}
}

func TestParseTHSIndustryCandidates(t *testing.T) {
	raw := `<a href="/thshy/detail/code/881155/">银行</a>
	<a class="board" href="https://q.10jqka.com.cn/thshy/detail/code/881116"><span>半导体及元件</span></a>
	<a href="/thshy/detail/code/881155/">银行</a>`
	items := ParseTHSIndustryCandidates(raw, "银行")
	if len(items) != 1 || items[0].Symbol != "th881155" || items[0].Name != "银行" {
		t.Fatalf("unexpected name candidates: %+v", items)
	}
	items = ParseTHSIndustryCandidates(raw, "881116")
	if len(items) != 1 || items[0].Symbol != "th881116" || items[0].Name != "半导体及元件" {
		t.Fatalf("unexpected code candidates: %+v", items)
	}
}
