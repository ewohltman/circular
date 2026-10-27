// Package web serves the meal-plan page and a small JSON API.
package web

import (
	"cmp"
	"context"
	"embed"
	"encoding/json/v2"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ewohltman/circular/internal/deals"
	"github.com/ewohltman/circular/internal/planner"
	"github.com/ewohltman/circular/internal/recipes"
	"github.com/ewohltman/circular/internal/store"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// Server wires deal sources, the planner, and the store to HTTP.
type Server struct {
	Sources []deals.Source
	Recipes []recipes.Recipe
	Store   *store.Store
	Log     *slog.Logger
	Now     func() time.Time

	tmpl      *template.Template
	refreshMu sync.Mutex
}

// Init parses templates. Call once before Handler.
func (s *Server) Init() error {
	if s.Now == nil {
		s.Now = time.Now
	}
	if s.Log == nil {
		s.Log = slog.Default()
	}
	t, err := template.New("").Funcs(template.FuncMap{
		"money": func(v float64) string { return fmt.Sprintf("$%.2f", v) },
		"date":  func(t time.Time) string { return t.Format("Mon Jan 2") },
		"join":  strings.Join,
		"storeClass": func(st deals.Store) string {
			return strings.ToLower(string(st))
		},
	}).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return err
	}
	s.tmpl = t
	return nil
}

// Handler returns the HTTP routes. Cross-origin browser requests to unsafe
// methods are rejected, so another site can't make a visitor's browser POST
// /refresh.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(staticFS, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /api/plan", s.handlePlanJSON)
	mux.HandleFunc("POST /refresh", s.handleRefresh)
	return http.NewCrossOriginProtection().Handler(mux)
}

// Refresh fetches deals from every source, rebuilds the plan, and saves it.
// A failing source is recorded but doesn't block the others.
func (s *Server) Refresh(ctx context.Context) (*store.Snapshot, error) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()

	terms := recipes.SearchTerms(s.Recipes)
	snap := &store.Snapshot{FetchedAt: s.Now()}
	for _, src := range s.Sources {
		ds, err := src.Fetch(ctx, terms)
		if err != nil {
			s.Log.Warn("deal source failed", "source", src.Name(), "err", err)
			snap.Errors = append(snap.Errors, fmt.Sprintf("%s: %v", src.Name(), err))
		}
		snap.Deals = append(snap.Deals, ds...)
	}
	snap.Deals = deals.FoodOnly(deals.Dedupe(snap.Deals))
	slices.SortStableFunc(snap.Deals, func(a, b deals.Deal) int {
		return cmp.Or(
			cmp.Compare(b.Store, a.Store), // ShopRite first
			cmp.Compare(b.Savings(), a.Savings()),
		)
	})
	snap.Plan = planner.Build(s.Recipes, snap.Deals, planner.WeekStart(s.Now()), planner.Options{})
	s.Log.Info("refreshed", "deals", len(snap.Deals), "meals", len(snap.Plan.Meals), "errors", len(snap.Errors))
	return snap, s.Store.Put(snap)
}

// RunWeekly refreshes whenever the stored snapshot is from an earlier circular
// week, checking hourly until ctx is cancelled.
func (s *Server) RunWeekly(ctx context.Context) {
	check := func() {
		cur := s.Store.Get()
		if cur != nil && !planner.WeekStart(cur.FetchedAt).Before(planner.WeekStart(s.Now())) {
			return
		}
		if _, err := s.Refresh(ctx); err != nil {
			s.Log.Error("weekly refresh", "err", err)
		}
	}
	check()
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			check()
		}
	}
}

type indexData struct {
	Snap   *store.Snapshot
	Stores []deals.Store
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	data := indexData{Snap: s.Store.Get(), Stores: []deals.Store{deals.ShopRite, deals.Costco}}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "index.html", data); err != nil {
		s.Log.Error("render", "err", err)
	}
}

func (s *Server) handlePlanJSON(w http.ResponseWriter, r *http.Request) {
	snap := s.Store.Get()
	if snap == nil {
		http.Error(w, "no plan yet; POST /refresh", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.MarshalWrite(w, snap, json.Deterministic(true)); err != nil {
		s.Log.Error("encode plan", "err", err)
	}
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	// The first refresh of a week reads ShopRite's price tags with OCR.
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	if _, err := s.Refresh(ctx); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
