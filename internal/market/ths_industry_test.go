package market

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
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

func TestParseTHSStockIndustryAndMatchDirectory(t *testing.T) {
	profile := `<div>主营业务：白酒生产</div><span>所属申万行业：</span><a href="/field/">白酒Ⅱ</a><span>| 概念行情</span>`
	industry := ParseTHSStockIndustry(profile)
	if industry != "白酒Ⅱ" {
		t.Fatalf("unexpected stock industry: %q", industry)
	}
	directory := `<a href="/thshy/detail/code/881121/">饮料制造</a>
	<a href="/thshy/detail/code/881125/"><span>白酒</span></a>`
	matched, ok := matchTHSIndustry(ParseTHSIndustryDirectory(directory), industry)
	if !ok || matched.Symbol != "th881125" || matched.Name != "白酒" {
		t.Fatalf("unexpected industry match: %+v, %v", matched, ok)
	}
}

func TestParseTHSStockIndustryUsesGenericFallback(t *testing.T) {
	profile := `<li>所属行业：<strong>银行Ⅲ</strong>；所属地域：上海</li>`
	if got := ParseTHSStockIndustry(profile); got != "银行Ⅲ" {
		t.Fatalf("unexpected fallback industry: %q", got)
	}
}

func TestTHSStockBoardClientFetchesIndependentIndustryFallback(t *testing.T) {
	encodeGB18030 := func(body string) string {
		encoded, err := simplifiedchinese.GB18030.NewEncoder().String(body)
		if err != nil {
			t.Fatal(err)
		}
		return encoded
	}
	originalClient := directHTTPClient
	t.Cleanup(func() { directHTTPClient = originalClient })
	directHTTPClient = &http.Client{Transport: httpRetryRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := ""
		switch request.URL.Path {
		case "/profile/600519":
			body = `<span>所属申万行业：</span><a>白酒Ⅱ</a>`
		case "/directory":
			body = `<a href="/thshy/detail/code/881125/">白酒</a>`
		case "/detail/881125":
			body = `<h3>白酒<span>881125</span></h3><span class="board-xj">1888.88</span><p class="board-zdf">8.88&nbsp;&nbsp;1.25%</p><dl><dt>资金净流入(亿)</dt><dd>2.30</dd></dl>`
		default:
			return &http.Response{StatusCode: http.StatusNotFound, Status: "404 Not Found", Header: make(http.Header), Body: io.NopCloser(strings.NewReader("missing"))}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(encodeGB18030(body)))}, nil
	})}
	t.Setenv("ASTOCK_THS_STOCK_PROFILE_URL", "https://example.test/profile/{code}")
	t.Setenv("ASTOCK_THS_INDUSTRY_DIRECTORY_URL", "https://example.test/directory")
	t.Setenv("ASTOCK_THS_INDUSTRY_API_URL", "https://example.test/detail/{code}")

	items, err := (THSStockBoardClient{}).FetchBoards(t.Context(), "sh600519")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Code != "th881125" || items[0].Name != "白酒" || items[0].Percent != 1.25 || math.Abs(items[0].MainNet-2.30e8) > 0.01 || !strings.Contains(items[0].Source, "同花顺") {
		t.Fatalf("unexpected THS stock-board fallback: %+v", items)
	}
}
