// Package web serves the meal-plan page and a small JSON API.
package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"sort"
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

// Handler returns the HTTP routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(staticFS, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /api/plan", s.handlePlanJSON)
	mux.HandleFunc("POST /refresh", s.handleRefresh)
	return mux
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
	snap.Deals = deals.Dedupe(snap.Deals)
	sort.SliceStable(snap.Deals, func(i, j int) bool {
		if snap.Deals[i].Store != snap.Deals[j].Store {
			return snap.Deals[i].Store > snap.Deals[j].Store // ShopRite first
		}
		return snap.Deals[i].Savings() > snap.Deals[j].Savings()
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
	json.NewEncoder(w).Encode(snap)
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	if _, err := s.Refresh(ctx); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
