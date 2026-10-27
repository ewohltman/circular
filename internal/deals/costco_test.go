package deals

import (
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// costcoFlight mimics the warehouse savings page's flight data: framework
// rows around the coupon sets, a stale date range in a disabled component,
// and the current book's dates.
const costcoFlight = `0:["$","div",null,{"children":"$L1"}]
1a:I[86869,[],"IconMark"]
4:{"disclaimer":"Item 1358148, Limit 5. Valid 10/23/24-11/17/24","enable_disclaimer":false}
5:{"markdown_text":"Valid 10/19/26 - 11/15/26 "}
6:["$","$L45c",null,{"couponSetId":"Apparel","couponCards":[{"cardDescription":"32 Degrees Heated Vest","itemNumber":"1872100","applicability":"warehouse_online","pricesOffCSObject":[{"prepend_text":"","value":["39.99"],"append_text":"After $10 OFF"}]}]}]
7:["$","$L45c",null,{"couponSetId":"Grocery","uniqueId":"blt21","couponCards":[` +
	`{"cardDescription":"Swanson Less Sodium Chicken Broth","itemNumber":"2071668","applicability":"warehouse_online","validDatesOverride":"","imageUrl":"https://example.test/broth.avif","productImageAdobe":"$undefined","pricesOffCSObject":[{"prepend_text":"Save","value":["2.50"],"append_text":"","has_off":false}]},` +
	`{"cardDescription":"Columbus Reduced Sodium Italian Dry Sliced Salame","itemNumber":"820590","applicability":"warehouse_only","validDatesOverride":"Limit 10.","pricesOffCSObject":[{"prepend_text":"","value":["9.99"],"append_text":" After $4 OFF"}]},` +
	`{"cardDescription":"Hickory Farms Traditional Gift Box","itemNumber":"1980652","applicability":"online_only","pricesOffCSObject":[{"prepend_text":"","value":["29.99"],"append_text":"After $20 OFF"}]},` +
	`{"cardDescription":"Kirkland Signature Salmon Bundle","itemNumber":"","applicability":"warehouse_only","pricesOffCSObject":[{"prepend_text":"Save","value":[" 1,200","1,500"],"append_text":""}]},` +
	`{"cardDescription":"Poppi Prebiotic Soda","itemNumber":"2043159, 2077776","applicability":"warehouse_online"}` +
	`]}]
`

// costcoPage embeds flight text the way Next.js does: split into chunks, each
// pushed as a JSON string literal.
func costcoPage(t *testing.T, flight string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("<html><body><script>(self.__next_f=self.__next_f||[]).push([0])</script>")
	half := len(flight) / 2
	for _, chunk := range []string{flight[:half], flight[half:]} {
		arg, err := json.Marshal([]any{1, chunk})
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, "<script>self.__next_f.push(%s)</script>", arg)
	}
	b.WriteString("</body></html>")
	return b.String()
}

func TestCostcoCoupons(t *testing.T) {
	page := costcoPage(t, costcoFlight)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "circular-meal-planner/") {
			t.Errorf("user agent %q", r.Header.Get("User-Agent"))
		}
		if _, err := w.Write([]byte(page)); err != nil {
			t.Error(err)
		}
	}))
	defer srv.Close()

	now := time.Date(2026, 10, 27, 12, 0, 0, 0, eastern)
	c := &CostcoCoupons{URL: srv.URL, Now: func() time.Time { return now }}
	got, err := c.Fetch(t.Context(), []string{"broth", "salame", "gift box", "salmon", "vest"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Deal{
		{ID: "2071668", Name: "Swanson Less Sodium Chicken Broth", Discount: 2.5, SaleStory: "Save $2.50", ImageURL: "https://example.test/broth.avif"},
		{ID: "820590", Name: "Columbus Reduced Sodium Italian Dry Sliced Salame", Price: 9.99, RegularPrice: 13.99, SaleStory: "$9.99 after $4.00 off · Limit 10."},
		{ID: "grocery-3", Name: "Kirkland Signature Salmon Bundle", Discount: 1200, SaleStory: "Save $1200.00"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d deals, want %d: %+v", len(got), len(want), got)
	}
	from := time.Date(2026, 10, 19, 0, 0, 0, 0, eastern)
	to := time.Date(2026, 11, 15, 23, 59, 59, 0, eastern)
	for i, w := range want {
		w.Store, w.Category, w.Source, w.ValidFrom, w.ValidTo = Costco, costcoFood, "costco.com", from, to
		if got[i] != w {
			t.Errorf("deal %d:\n got %+v\nwant %+v", i, got[i], w)
		}
	}
	if got[1].Savings() != 4 || got[0].Savings() != 2.5 {
		t.Errorf("savings = %v, %v; want 4, 2.5", got[1].Savings(), got[0].Savings())
	}
}

func TestCostcoValidityFallsBackToLatest(t *testing.T) {
	after := time.Date(2026, 12, 1, 0, 0, 0, 0, eastern)
	from, to, ok := costcoValidity(costcoFlight, after)
	if !ok || from.Year() != 2026 || to.Month() != time.November {
		t.Errorf("costcoValidity = %v, %v, %v; want the 2026 book", from, to, ok)
	}
}

func TestCostcoPageWithoutCoupons(t *testing.T) {
	if _, err := nextFlight("<html>Access Denied</html>"); err == nil {
		t.Error("want an error for a page without flight data")
	}
	if _, err := costcoSets("0:[]\n"); err == nil {
		t.Error("want an error for flight data without coupon sets")
	}
}
