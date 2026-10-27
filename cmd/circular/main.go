// Command circular serves a weekly meal plan built from ShopRite and Costco deals.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ewohltman/circular/internal/deals"
	"github.com/ewohltman/circular/internal/pricetag"
	"github.com/ewohltman/circular/internal/recipes"
	"github.com/ewohltman/circular/internal/store"
	"github.com/ewohltman/circular/internal/web"
)

func main() {
	var (
		addr      = flag.String("addr", envOr("CIRCULAR_ADDR", ":8080"), "listen address")
		zip       = flag.String("zip", envOr("CIRCULAR_ZIP", ""), "postal code; picks the ShopRite store there when -shoprite-store is unset")
		storeNum  = flag.String("shoprite-store", envOr("CIRCULAR_SHOPRITE_STORE", ""), "ShopRite store number, e.g. 524 for North Brunswick")
		tesseract = flag.String("tesseract", envOr("CIRCULAR_TESSERACT", "tesseract"), "Tesseract binary for reading ShopRite prices; \"\" skips prices")
		dataPath  = flag.String("data", envOr("CIRCULAR_DATA", "data/snapshot.json"), "where to cache deals and the plan")
		recipeF   = flag.String("recipes", envOr("CIRCULAR_RECIPES", ""), "recipe catalog JSON (default: built-in)")
		files     = flag.String("deals-file", envOr("CIRCULAR_DEALS_FILES", ""), "comma-separated JSON deal files")
		offline   = flag.Bool("offline", false, "skip costco.com and ShopRite's circular and use only -deals-file (handy for demos)")
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
		if *storeNum == "" && *zip == "" {
			log.Error("set -shoprite-store or -zip (or CIRCULAR_SHOPRITE_STORE / CIRCULAR_ZIP), or use -offline")
			os.Exit(2)
		}
		sr := &deals.ShopRiteCircular{Store: *storeNum, PostalCode: *zip}
		if *tesseract != "" {
			if _, err := exec.LookPath(*tesseract); err != nil {
				log.Warn("tesseract not found; ShopRite prices will be unknown", "err", err)
			} else {
				sr.Tags = &pricetag.Reader{Tesseract: *tesseract}
			}
		}
		sources = append(sources, sr, &deals.CostcoCoupons{})
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
