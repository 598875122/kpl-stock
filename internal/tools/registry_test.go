package tools

import "testing"

func TestDefaultRegistryHasRequiredHermesTools(t *testing.T) {
	registry := DefaultRegistry()
	for _, name := range []string{"market.sentiment", "stock.kline", "auction.limit_bid"} {
		if _, ok := registry.Get(name); !ok {
			t.Fatalf("missing tool %s", name)
		}
	}
	if got := len(registry.List()); got < 85 {
		t.Fatalf("tool count = %d, want at least 85", got)
	}
}

func TestToolPrepareValidatesPathAndQuery(t *testing.T) {
	registry := DefaultRegistry()
	tool, ok := registry.Get("stock.kline")
	if !ok {
		t.Fatal("missing stock.kline")
	}

	prepared, err := tool.Prepare(map[string]any{
		"code":  "300857",
		"type":  1,
		"begin": "20260101",
		"end":   "20260430",
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Path != "/api/stock/kline/300857" {
		t.Fatalf("path = %q", prepared.Path)
	}
	if prepared.Query.Get("type") != "1" {
		t.Fatalf("type query missing")
	}
}

func TestToolPrepareRejectsUnknownParameter(t *testing.T) {
	registry := DefaultRegistry()
	tool, ok := registry.Get("market.sentiment")
	if !ok {
		t.Fatal("missing market.sentiment")
	}

	if _, err := tool.Prepare(map[string]any{"unexpected": "x"}); err == nil {
		t.Fatal("expected unknown parameter error")
	}
}
