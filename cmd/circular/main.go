// Command circular serves a weekly meal plan built from ShopRite and Costco deals.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ewohltman/circular/internal/deals"
	"github.com/ewohltman/circular/internal/recipes"
	"github.com/ewohltman/circular/internal/store"
	"github.com/ewohltman/circular/internal/web"
)

func main() {
	var (
		addr     = flag.String("addr", envOr("CIRCULAR_ADDR", ":8080"), "listen address")
		zip      = flag.String("zip", envOr("CIRCULAR_ZIP", ""), "postal code used to find your local circulars")
		dataPath = flag.String("data", envOr("CIRCULAR_DATA", "data/snapshot.json"), "where to cache deals and the plan")
		recipeF  = flag.String("recipes", envOr("CIRCULAR_RECIPES", ""), "recipe catalog JSON (default: built-in)")
		files    = flag.String("deals-file", envOr("CIRCULAR_DEALS_FILES", ""), "comma-separated JSON deal files, e.g. a Costco coupon book")
		offline  = flag.Bool("offline", false, "skip Flipp and use only -deals-file (handy for demos)")
	)
	flag.Parse()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	rs, err := recipes.Load(*recipeF)
	if err != nil {
		log.Error("load recipes", "err", err)
		os.Exit(1)
	}
	st, err := store.Open(*dataPath)
	if err != nil {
		log.Error("open store", "err", err)
		os.Exit(1)
	}

	var sources []deals.Source
	if !*offline {
		if *zip == "" {
			log.Error("set -zip (or CIRCULAR_ZIP) to your postal code, or use -offline")
			os.Exit(2)
		}
		sources = append(sources, &deals.Flipp{
			PostalCode: *zip,
			Stores:     []deals.Store{deals.ShopRite, deals.Costco},
			Delay:      300 * time.Millisecond,
		})
	}
	for p := range strings.SplitSeq(*files, ",") {
		if p = strings.TrimSpace(p); p != "" {
			sources = append(sources, &deals.File{Path: p})
		}
	}
	if len(sources) == 0 {
		log.Error("no deal sources configured")
		os.Exit(2)
	}

	srv := &web.Server{Sources: sources, Recipes: rs, Store: st, Log: log}
	if err := srv.Init(); err != nil {
		log.Error("init", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go srv.RunWeekly(ctx)

	hs := &http.Server{Addr: *addr, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := hs.Shutdown(shutdown); err != nil {
			log.Warn("shutdown", "err", err)
		}
	}()
	log.Info("listening", "addr", *addr, "sources", len(sources), "recipes", len(rs))
	if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("serve", "err", err)
		os.Exit(1)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
