package market

import (
	"context"
	"math"
	"os"
	"strings"
	"testing"
	"time"
)

func TestParseSinaGlobalIndexPayloadNormalizesMarketLayouts(t *testing.T) {
	raw := `var hq_str_rt_hkHSI="HSI,恒生指数,25346.900,25471.150,25537.190,25315.320,25479.139,7.990,0.030,0,0,0,0,0,0,0,0,2026/08/19,15:24:10";
var hq_str_b_NKY="日经225指数,65326.2000,-2134.53,-3.16,2:12 AM,14:12:00,2026-08-19,14:30:01,66812.2700,67460.7300,66833.5100,65133.9800,0";
var hq_str_gb_dji="道琼斯,53343.3984,-0.22,2026-08-19 04:40:25,-116.3800,53354.4297,53478.7617,53256.3398,54744.3281,44579.0312,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,Aug 18,53459.7812";
var hq_str_gb_dia="道指ETF,532.9100,-0.24,2026-08-19 09:38:56,-1.2800,533.3800,534.2600,532.0200,546.7500,438.9660,3825692,3403746,0,0.00,--,0,0,0,0,0,0,532.8950,-0.00,-0.0150,Aug 18 07:41PM EDT,Aug 18 04:00PM EDT,534.1900,346866";`
	items := ParseSinaGlobalIndexPayload(raw)
	if len(items) != len(globalIndexSpecs) {
		t.Fatalf("expected %d stable rows, got %d", len(globalIndexSpecs), len(items))
	}
	hsi := items[0]
	if hsi.Name != "恒生指数" || hsi.Current != 25479.139 || hsi.Delta != 7.99 || hsi.Percent != .03 || hsi.QuoteTime != "2026-08-19 15:24:10" {
		t.Fatalf("unexpected Hong Kong index: %#v", hsi)
	}
	nikkei := items[3]
	if nikkei.Name != "日经225" || nikkei.Current != 65326.2 || nikkei.Open != 66812.27 || nikkei.PreviousClose != 67460.73 || nikkei.QuoteTime != "2026-08-19 14:12:00" {
		t.Fatalf("unexpected Asian index: %#v", nikkei)
	}
	dow := items[7]
	if dow.Name != "道琼斯" || dow.Current != 53343.3984 || dow.Delta != -116.38 || dow.Percent != -.22 || dow.PreviousClose != 53459.7812 {
		t.Fatalf("unexpected US index: %#v", dow)
	}
	if dow.Extended == nil || dow.Extended.Session != "盘后" || dow.Extended.Symbol != "DIA" || dow.Extended.Price != 532.895 || dow.Extended.Delta != -.015 || dow.Extended.QuoteTime != "Aug 18 07:41PM EDT" {
		t.Fatalf("unexpected US extended-hours proxy: %#v", dow.Extended)
	}
	if !math.IsNaN(items[4].Current) || items[4].Name != "东证指数" {
		t.Fatalf("missing records should retain labeled placeholders: %#v", items[4])
	}
}

func TestGlobalIndexAddressContainsEveryConfiguredIndex(t *testing.T) {
	address := globalIndexAddress("https://example.test/list=")
	for _, wanted := range []string{"rt_hkHSI", "b_NKY", "b_TOPIX", "b_KOSPI", "b_KOSDAQ", "gb_dji", "gb_dia", "gb_ixic", "gb_qqq", "gb_inx", "gb_spy"} {
		if !strings.Contains(address, wanted) {
			t.Fatalf("address missing %s: %s", wanted, address)
		}
	}
}

func TestGlobalExtendedSessionClassifiesUSMarketTime(t *testing.T) {
	tests := map[string]string{
		"Aug 19 04:01AM EDT": "盘前",
		"Aug 19 09:29AM EDT": "盘前",
		"Aug 19 04:01PM EDT": "盘后",
		"Aug 19 07:59PM EDT": "盘后",
		"unknown":            "延长",
	}
	for value, wanted := range tests {
		if got := globalExtendedSession(value); got != wanted {
			t.Fatalf("globalExtendedSession(%q) = %q, want %q", value, got, wanted)
		}
	}
}

func TestSinaGlobalIndexIntegration(t *testing.T) {
	if os.Getenv("ASTOCK_INTEGRATION") != "1" {
		t.Skip("set ASTOCK_INTEGRATION=1 to call Sina's global-index endpoint")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	items, err := (SinaGlobalIndexClient{}).FetchGlobalIndices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != len(globalIndexSpecs) {
		t.Fatalf("incomplete stable global-index rows: %#v", items)
	}
	available := 0
	extended := 0
	for _, item := range items {
		if !math.IsNaN(item.Current) {
			available++
			if item.Name == "" || item.Region == "" || item.QuoteTime == "" {
				t.Fatalf("incomplete available global index: %#v", item)
			}
		}
		if item.Extended != nil {
			extended++
		}
	}
	if available < 8 {
		t.Fatalf("expected at least 8 live global indices, got %d: %#v", available, items)
	}
	if extended != 3 {
		t.Fatalf("expected three US ETF extended-hours proxies, got %d: %#v", extended, items)
	}
	t.Logf("received %d/%d global indices and %d extended-hours proxies", available, len(items), extended)
}
