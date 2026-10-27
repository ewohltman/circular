package deals

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ewohltman/circular/internal/pricetag"
)

// fakeTags returns canned tags by title and records the tiles it was given.
type fakeTags struct {
	mu    sync.Mutex
	tags  map[string]pricetag.Tag
	tiles map[string]image.Rectangle
}

func (f *fakeTags) Read(_ context.Context, tile image.Image, title string) (pricetag.Tag, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tiles[title] = tile.Bounds()
	tag, ok := f.tags[title]
	if !ok {
		return pricetag.Tag{}, pricetag.ErrNoTitle
	}
	return tag, nil
}

// redPepper serves a small circular for store 524 and counts page downloads.
func redPepper(t *testing.T) (*httptest.Server, *int) {
	t.Helper()
	page := image.NewRGBA(image.Rect(0, 0, 200, 400))
	draw.Draw(page, page.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, page, nil); err != nil {
		t.Fatal(err)
	}
	images := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := ""
		switch r.URL.Path {
		case "/client/4573/catalogue/geo_location/json":
			body = `[
				{"field_store_id":"524","field_store_name":"ShopRite of North Brunswick","field_zipcode":"08902","field_version":"100"},
				{"field_store_id":"524","field_store_name":"ShopRite of North Brunswick","field_zipcode":"08902","field_version":"101"},
				{"field_store_id":"600","field_store_name":"ShopRite of Elsewhere","field_zipcode":"08902","field_version":"200"},
				{"field_store_id":"548","field_store_name":"ShopRite of Aberdeen, MD","field_zipcode":"21001","field_version":"300"}]`
		case "/client/4573/catalogues/json":
			body = `[
				{"nid_1":"100","title":"Week of 10/25","start":"2026-10-22T00:01","finish":"2026-10-31T23:59"},
				{"nid_1":"101","title":"Week of 10/18","start":"2026-10-15T00:00:00","finish":"2026-10-24T23:59:00"},
				{"nid_1":"200","title":"Week of 10/25","start":"2026-10-22T00:01","finish":"2026-10-31T23:59"}]`
		case "/catalogue/100/page-images/json":
			body = `[{"page":"1","image":"` + srv.URL + `/pages/1.jpeg"}]`
		case "/rpms_catalogue/100/page/0/1/regions":
			body = `{"0":[],"1":[
				{"id":"r1","field_region_type":"product","field_product_title":"Bowl & Basket Boneless Chicken Thighs","page":"1","coords":"10.00,10.00,1000001,40.00,20.00","field_product_image_url":["https://example.test/r1.png"]},
				{"id":"r2","field_region_type":"product","field_product_title":"Hass Avocados","page":"1","coords":"50.00,10.00,1000001,40.00,20.00","field_product_image_url":[]},
				{"id":"r3","field_region_type":"product","field_product_title":"Skippy Peanut Butter","page":"1","coords":"10.00,40.00,1000001,40.00,20.00","field_product_image_url":[]},
				{"id":"r4","field_region_type":"link","field_product_title":"","page":"1","coords":"0,90,1000001,100,10","field_product_image_url":[]}]}`
		case "/pages/1.jpeg":
			images++
			if _, err := w.Write(jpg.Bytes()); err != nil {
				t.Error(err)
			}
			return
		default:
			http.NotFound(w, r)
			return
		}
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "circular-meal-planner/") {
			t.Errorf("user agent %q", r.Header.Get("User-Agent"))
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &images
}

func TestShopRiteCircular(t *testing.T) {
	srv, images := redPepper(t)
	tags := &fakeTags{
		tags: map[string]pricetag.Tag{
			"Bowl & Basket Boneless Chicken Thighs": {Price: 2.56, Save: 0.5, Unit: "lb", Story: "$2.56/lb, save $0.50/lb"},
		},
		tiles: map[string]image.Rectangle{},
	}
	sr := &ShopRiteCircular{
		BaseURL: srv.URL,
		Store:   "524",
		Tags:    tags,
		Now:     func() time.Time { return time.Date(2026, 10, 27, 12, 0, 0, 0, eastern) },
	}
	terms := []string{"chicken thigh", "avocado"}
	got, err := sr.Fetch(t.Context(), terms)
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 10, 25, 0, 0, 0, 0, eastern) // the sale week, not the preview start
	to := time.Date(2026, 10, 31, 23, 59, 0, 0, eastern)
	want := []Deal{
		{ID: "r1", Name: "Bowl & Basket Boneless Chicken Thighs", Price: 2.56, RegularPrice: 3.06, Discount: 0.5, Unit: "lb", SaleStory: "$2.56/lb, save $0.50/lb", ImageURL: "https://example.test/r1.png"},
		{ID: "r2", Name: "Hass Avocados"}, // OCR couldn't find the item in its tile
	}
	if len(got) != len(want) {
		t.Fatalf("got %d deals, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		w.Store, w.Source, w.ValidFrom, w.ValidTo = ShopRite, "redpepper", from, to
		if got[i] != w {
			t.Errorf("deal %d:\n got %+v\nwant %+v", i, got[i], w)
		}
	}
	if b := tags.tiles["Bowl & Basket Boneless Chicken Thighs"]; b != image.Rect(20, 40, 100, 120) {
		t.Errorf("tile = %v, want (20,40)-(100,120)", b)
	}

	// A second refresh reuses the readings without downloading pages again.
	tags.tiles = map[string]image.Rectangle{}
	if _, err := sr.Fetch(t.Context(), terms); err != nil {
		t.Fatal(err)
	}
	if len(tags.tiles) != 0 || *images != 1 {
		t.Errorf("second fetch read %d tiles and downloaded %d pages; want 0 and 1 in total", len(tags.tiles), *images)
	}
}

func TestShopRiteStoreByZip(t *testing.T) {
	srv, _ := redPepper(t)
	now := func() time.Time { return time.Date(2026, 10, 27, 12, 0, 0, 0, eastern) }
	sr := &ShopRiteCircular{BaseURL: srv.URL, PostalCode: "08902", Now: now}
	got, err := sr.Fetch(t.Context(), []string{"avocado"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "r2" || got[0].Price != 0 {
		t.Errorf("got %+v, want the avocados from store 524 without a price", got)
	}

	sr = &ShopRiteCircular{BaseURL: srv.URL, PostalCode: "99999", Now: now}
	if _, err := sr.Fetch(t.Context(), []string{"avocado"}); err == nil {
		t.Error("want an error for a postal code without a store")
	}
}

func TestSaleWeek(t *testing.T) {
	start := time.Date(2026, 12, 29, 0, 1, 0, 0, eastern)
	got := saleWeek(rpCatalogue{Title: "Week of 1/3", Start: start})
	if !got.Equal(time.Date(2027, 1, 3, 0, 0, 0, 0, eastern)) {
		t.Errorf("saleWeek across new year = %v", got)
	}
	if got := saleWeek(rpCatalogue{Title: "Wellness Your Way", Start: start}); !got.Equal(start) {
		t.Errorf("saleWeek without a week in the title = %v, want the start", got)
	}
}
