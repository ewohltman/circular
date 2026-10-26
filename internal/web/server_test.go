package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ewohltman/circular/internal/deals"
	"github.com/ewohltman/circular/internal/recipes"
	"github.com/ewohltman/circular/internal/store"
)

func TestRefreshAndRender(t *testing.T) {
	rs, err := recipes.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "snap.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv := &Server{
		Sources: []deals.Source{&deals.File{Path: "../../testdata/sample_deals.json"}},
		Recipes: rs,
		Store:   st,
		Now:     func() time.Time { return time.Date(2026, 10, 26, 12, 0, 0, 0, time.Local) },
	}
	if err := srv.Init(); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Empty state first.
	body := get(t, ts.URL+"/")
	if !strings.Contains(body, "Refresh deals") {
		t.Fatalf("empty page missing refresh button")
	}

	resp, err := http.Post(ts.URL+"/refresh", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK { // after redirect to /
		t.Fatalf("refresh status %d", resp.StatusCode)
	}

	body = get(t, ts.URL+"/")
	for _, want := range []string{"dinners", "ShopRite", "Costco", "Shopping list"} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
	if !strings.Contains(get(t, ts.URL+"/api/plan"), `"meals"`) {
		t.Errorf("api/plan missing meals")
	}
}

func TestRefreshRejectsCrossOrigin(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "snap.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv := &Server{Store: st}
	if err := srv.Init(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/refresh", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site POST /refresh: status %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func get(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}
