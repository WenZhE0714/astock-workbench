package market

import (
	"errors"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestInspectSymbol(t *testing.T) {
	cases := map[string]string{
		"600519":   "sh600519",
		"000001":   "sz000001",
		"sh600519": "sh600519",
	}
	for input, expected := range cases {
		actual, status := InspectSymbol(input)
		if status != "ok" || actual != expected {
			t.Fatalf("%s: got %s/%s", input, actual, status)
		}
	}
}

func TestInspectAssetSupportsConvertibleBondsAndBoards(t *testing.T) {
	cases := map[string]struct {
		symbol string
		kind   string
	}{
		"113001":   {symbol: "sh113001", kind: string(domain.AssetKindConvertibleBond)},
		"123001":   {symbol: "sz123001", kind: string(domain.AssetKindConvertibleBond)},
		"BK0423":   {symbol: "BK0423", kind: string(domain.AssetKindSector)},
		"bk0423":   {symbol: "BK0423", kind: string(domain.AssetKindSector)},
		"881155":   {symbol: "th881155", kind: string(domain.AssetKindSector)},
		"th881155": {symbol: "th881155", kind: string(domain.AssetKindSector)},
		"600519":   {symbol: "sh600519", kind: string(domain.AssetKindStock)},
	}
	for input, expected := range cases {
		symbol, status := InspectAsset(input)
		if status != "ok" || symbol != expected.symbol || string(AssetKindOf(symbol)) != expected.kind {
			t.Fatalf("%s: got %s/%s/%s", input, symbol, status, AssetKindOf(symbol))
		}
	}
	if _, status := InspectAsset("sz113001"); status != "invalid" {
		t.Fatalf("mismatched convertible-bond exchange should be invalid, got %s", status)
	}
}

func TestCandidateParsers(t *testing.T) {
	sina := ParseSinaCandidates(`var suggestvalue="贵州茅台,11,600519,sh600519,x,x,x,1,1;贵州茅台债,23,123,sh123,x,x,x,1,1";`)
	if len(sina) != 1 || sina[0].Symbol != "sh600519" {
		t.Fatalf("unexpected Sina result: %#v", sina)
	}
	tencent := ParseTencentCandidates(`v_hint="sh~600519~\u8d35\u5dde\u8305\u53f0~gzmt~GP-A^";`)
	if len(tencent) != 1 || tencent[0].Name != "贵州茅台" {
		t.Fatalf("unexpected Tencent result: %#v", tencent)
	}
	convertible := ParseTencentCandidates(`v_hint="sh~113001~中行转债~zhzz~KZZ-A^";`)
	if len(convertible) != 1 || convertible[0].Symbol != "sh113001" {
		t.Fatalf("unexpected convertible-bond result: %#v", convertible)
	}
}

func TestChooseCandidateRequiresSelectionWhenSourcesReturnMultipleMatches(t *testing.T) {
	items := []domain.Candidate{
		{Symbol: "th881155", Name: "银行"},
		{Symbol: "sh601288", Name: "农业银行"},
		{Symbol: "sh601398", Name: "工商银行"},
	}
	if symbol, err := chooseCandidate("银行", items); symbol != "" || err == nil {
		t.Fatalf("expected ambiguous bank search, got %q/%v", symbol, err)
	}
	var ambiguous *AmbiguousNameError
	_, err := chooseCandidate("银行", []domain.Candidate{
		{Symbol: "sh601288", Name: "农业银行"},
		{Symbol: "BK1283", Name: "银行"},
		{Symbol: "th881155", Name: "银行"},
	})
	if !errors.As(err, &ambiguous) || len(ambiguous.Candidates) != 3 || ambiguous.Candidates[0].Symbol != "th881155" || ambiguous.Candidates[1].Symbol != "BK1283" || ambiguous.Candidates[2].Symbol != "sh601288" {
		t.Fatalf("industry candidates should be prioritized: %#v / %v", ambiguous, err)
	}
	if symbol, err := chooseCandidate("银行", []domain.Candidate{{Symbol: "th881155", Name: "银行"}, {Symbol: "th881155", Name: "银行"}}); err != nil || symbol != "th881155" {
		t.Fatalf("duplicate source result should resolve uniquely, got %q/%v", symbol, err)
	}
}

func TestChooseCandidatePrefersExactNameOverSubstringMatches(t *testing.T) {
	items := []domain.Candidate{
		{Symbol: "sh601186", Name: "中国铁建"},
		{Symbol: "sh601669", Name: "中国电建"},
		{Symbol: "sh601868", Name: "中国能建"},
		{Symbol: "sh601611", Name: "中国核建"},
		{Symbol: "sh601800", Name: "中国交建"},
		{Symbol: "sz000927", Name: "中国铁物"},
	}
	if symbol, err := chooseCandidate("中国铁建", items); err != nil || symbol != "sh601186" {
		t.Fatalf("exact stock name should beat substring matches, got %q/%v", symbol, err)
	}
	if symbol, err := chooseCandidate("中国", items); err == nil || symbol != "" {
		t.Fatalf("broad partial name should remain ambiguous, got %q/%v", symbol, err)
	}
}
