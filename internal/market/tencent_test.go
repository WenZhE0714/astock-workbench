package market

import "testing"

func mockQuotePayload() string {
	fields := make([]string, 53)
	fields[1] = "贵州茅台"
	fields[2] = "600519"
	fields[3] = "1418.00"
	fields[4] = "1400.00"
	fields[5] = "1402.00"
	fields[9] = "1417.00"
	fields[10] = "8"
	fields[19] = "1418.00"
	fields[20] = "5"
	fields[30] = "20260807145959"
	fields[31] = "18.00"
	fields[32] = "1.29"
	fields[33] = "1420.00"
	fields[34] = "1398.00"
	fields[36] = "12345"
	fields[37] = "175000"
	fields[38] = "0.10"
	fields[39] = "19.77"
	fields[43] = "1.57"
	fields[44] = "16351.07"
	fields[45] = "16000.00"
	fields[46] = "7.02"
	fields[47] = "1540.00"
	fields[48] = "1260.00"
	fields[49] = "0.84"
	fields[51] = "1409.50"
	fields[52] = "15.01"
	return `v_sh600519="` + joinFields(fields) + `";`
}

func joinFields(fields []string) string {
	result := ""
	for index, field := range fields {
		if index > 0 {
			result += "~"
		}
		result += field
	}
	return result
}

func TestParseQuotePayload(t *testing.T) {
	quotes := ParseQuotePayload(mockQuotePayload())
	if len(quotes) != 1 {
		t.Fatalf("expected one quote, got %d", len(quotes))
	}
	if quotes[0].Name != "贵州茅台" || quotes[0].Percent != 1.29 {
		t.Fatalf("unexpected quote: %#v", quotes[0])
	}
	if quotes[0].QuoteTime != "2026-08-07 14:59:59" {
		t.Fatalf("unexpected time: %q", quotes[0].QuoteTime)
	}
	if quotes[0].PETTM != "19.77" || quotes[0].PB != "7.02" || quotes[0].MarketCap != 16351.07 {
		t.Fatalf("extended fields were not parsed: %#v", quotes[0])
	}
	if quotes[0].VolumeRatio != "0.84" || quotes[0].AveragePrice != "1409.50" {
		t.Fatalf("market fields were not parsed: %#v", quotes[0])
	}
}

func TestParseQuotePayloadIncludesBroadMarketIndices(t *testing.T) {
	fields := make([]string, 53)
	fields[1] = "上证指数"
	fields[2] = "000001"
	fields[3] = "3635.13"
	fields[4] = "3626.36"
	fields[5] = "3628.00"
	fields[30] = "20260807150000"
	fields[31] = "8.77"
	fields[32] = "0.24"
	fields[33] = "3640.00"
	fields[34] = "3610.00"
	payload := `v_sh000001="` + joinFields(fields) + `";`
	quotes := ParseQuotePayload(payload)
	if len(quotes) != 1 || quotes[0].Symbol != "sh000001" || quotes[0].Percent != 0.24 {
		t.Fatalf("unexpected index quote: %#v", quotes)
	}
}

func TestParseQuotePayloadIncludesBeijingAmountIndex(t *testing.T) {
	fields := make([]string, 53)
	fields[1] = "北证50"
	fields[2] = "899050"
	fields[3] = "1122.88"
	fields[30] = "20260810150000"
	fields[33] = "1142.03"
	fields[34] = "1121.77"
	fields[37] = "1576946.9231"
	payload := `v_bj899050="` + joinFields(fields) + `";`
	quotes := ParseQuotePayload(payload)
	if len(quotes) != 1 || quotes[0].Symbol != "bj899050" || quotes[0].Amount != 1576946.9231 {
		t.Fatalf("unexpected Beijing quote: %#v", quotes)
	}
}
