package deals

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFlippFetchFiltersStores(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("postal_code") != "07052" {
			t.Errorf("missing postal code: %s", r.URL.RawQuery)
		}
		w.Write([]byte(`{"items":[
			{"flyer_item_id":1,"name":"Boneless Chicken Breast","merchant_name":"ShopRite","current_price":1.99,"post_price_text":"lb","valid_from":"2026-10-23T04:00:00+00:00","valid_to":"2026-10-30T03:59:59+00:00"},
			{"flyer_item_id":2,"name":"Chicken Thighs","merchant_name":"Stop & Shop","current_price":2.49},
			{"flyer_item_id":3,"name":"Kirkland Chicken","merchant_name":"Costco","current_price":"12.99","original_price":"15.99"}
		]}`))
	}))
	defer srv.Close()

	f := &Flipp{BaseURL: srv.URL, PostalCode: "07052", Stores: []Store{ShopRite, Costco}}
	got, err := f.Fetch(context.Background(), []string{"chicken", "chicken"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 deals after filter+dedupe, got %d: %+v", len(got), got)
	}
	if got[0].Store != ShopRite || got[0].Price != 1.99 || got[0].Unit != "lb" {
		t.Errorf("bad ShopRite deal: %+v", got[0])
	}
	if got[1].Store != Costco || got[1].Savings() < 2.99 || got[1].Savings() > 3.01 {
		t.Errorf("bad Costco deal: %+v (savings %.2f)", got[1], got[1].Savings())
	}
}
