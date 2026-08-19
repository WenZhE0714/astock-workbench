package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestWatchGlobalMarketsKeepsLastSnapshotOnRefreshFailure(t *testing.T) {
	state := watchGlobalMarkets{}
	state.open()
	state.beginRefresh()
	state.complete([]domain.GlobalIndex{{Symbol: "gb_inx", Name: "标普500", Current: 7691.76}})
	refreshedAt := state.refreshedAt
	state.beginRefresh()
	state.fail(errors.New("timeout"))
	if !state.viewing || state.loading || len(state.indices) != 1 || state.refreshedAt != refreshedAt {
		t.Fatalf("failed refresh should keep the last snapshot: %#v", state)
	}
	if status := state.status(false); !strings.Contains(status, "保留") || !strings.Contains(status, "timeout") {
		t.Fatalf("unexpected retained-data status: %s", status)
	}
}
