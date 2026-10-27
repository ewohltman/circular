package store

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ewohltman/circular/internal/deals"
	"github.com/ewohltman/circular/internal/planner"
)

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snap.json")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 10, 27, 9, 30, 0, 0, time.UTC)
	snap := &Snapshot{
		FetchedAt: when,
		Deals:     []deals.Deal{{ID: "1", Store: deals.Costco, Name: "Swanson Chicken Broth", Discount: 2.5}},
		Plan: planner.Plan{
			WeekOf: when,
			ByStore: map[deals.Store][]planner.ShoppingItem{
				deals.ShopRite: {{Name: "chicken thighs"}},
				deals.Costco:   {{Name: "broth"}},
			},
		},
	}
	if err := st.Put(snap); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Map keys are sorted, so writing the same snapshot again is byte-identical.
	if err := st.Put(snap); err != nil {
		t.Fatal(err)
	}
	if second, _ := os.ReadFile(path); !bytes.Equal(first, second) {
		t.Error("snapshot output is not deterministic")
	}
	if i, j := bytes.Index(first, []byte(`"Costco"`)), bytes.Index(first, []byte(`"ShopRite"`)); i < 0 || j < i {
		t.Errorf("by_store keys not sorted:\n%s", first)
	}

	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got := again.Get()
	if !got.FetchedAt.Equal(when) || len(got.Deals) != 1 || got.Deals[0].Discount != 2.5 || len(got.Plan.ByStore[deals.ShopRite]) != 1 {
		t.Errorf("round trip lost data: %+v", got)
	}
}
