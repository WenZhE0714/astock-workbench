package market

import (
	"context"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

const marketRankingFixture = `{"rc":0,"data":{"diff":[{"f2":43.33,"f3":15.82,"f12":"688166","f13":1,"f14":"博瑞医药","f22":3.02,"f100":"化学制药"},{"f2":"25.90","f3":"4.48","f12":"300122","f13":0,"f14":"智飞生物","f22":"2.21","f100":"生物制品"},{"f2":null,"f3":"-","f12":"bad","f13":0,"f14":"无效","f22":null,"f100":"-"}]}}`

type rankingRoundTripFunc func(*http.Request) (*http.Response, error)

func (function rankingRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestParseMarketRankingPayloadKeepsIndustryAndMetrics(t *testing.T) {
	items := ParseMarketRankingPayload(marketRankingFixture)
	if len(items) != 2 {
		t.Fatalf("expected two ranking rows, got %d", len(items))
	}
	if items[0].Symbol != "sh688166" || items[0].Name != "博瑞医药" || items[0].Price != 43.33 ||
		items[0].Percent != 15.82 || items[0].Speed != 3.02 || items[0].Industry != "化学制药" {
		t.Fatalf("unexpected Shanghai ranking row: %#v", items[0])
	}
	if items[1].Symbol != "sz300122" || items[1].Industry != "生物制品" || math.IsNaN(items[1].Price) {
		t.Fatalf("unexpected Shenzhen ranking row: %#v", items[1])
	}
}

func TestMarketRankingAddressUsesExpectedSort(t *testing.T) {
	tests := []struct {
		kind   domain.MarketRankingKind
		metric string
		order  string
	}{
		{kind: domain.MarketRankingGainers, metric: "f3", order: "1"},
		{kind: domain.MarketRankingLosers, metric: "f3", order: "0"},
		{kind: domain.MarketRankingRapidRise, metric: "f22", order: "1"},
	}
	for _, test := range tests {
		address, err := marketRankingAddress("https://example.test/api", test.kind, 20)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := url.Parse(address)
		if err != nil {
			t.Fatal(err)
		}
		query := parsed.Query()
		if query.Get("fid") != test.metric || query.Get("po") != test.order || query.Get("pz") != "20" || query.Get("fs") != marketRankingUniverse {
			t.Fatalf("unexpected %s ranking query: %s", test.kind, parsed.RawQuery)
		}
	}
}

func TestFetchMarketRankingUsesConfiguredEndpoint(t *testing.T) {
	originalClient := httpClient
	httpClient = &http.Client{Transport: rankingRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Query().Get("fid") != "f22" || request.URL.Query().Get("pz") != "20" {
			t.Errorf("unexpected ranking request: %s", request.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader(marketRankingFixture)),
			Header:     make(http.Header),
		}, nil
	})}
	t.Cleanup(func() { httpClient = originalClient })
	t.Setenv("ASTOCK_MARKET_RANK_API_URL", "https://example.test/rank")

	items, err := (EastmoneyClient{}).FetchMarketRanking(context.Background(), domain.MarketRankingRapidRise, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Symbol != "sh688166" {
		t.Fatalf("unexpected fetched ranking: %#v", items)
	}
}
