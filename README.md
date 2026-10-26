# circular

A small Go web app that pulls this week's ShopRite and Costco deals and builds
a seven-night dinner plan and shopping list around them.

## Run it

```sh
# Live data (needs internet access to Flipp)
go run ./cmd/circular -zip 07052

# Offline demo with the bundled sample deals
go run ./cmd/circular -offline -deals-file testdata/sample_deals.json
```

Open http://localhost:8080 and press **Refresh deals**. The app also refreshes
on its own when a new circular week starts (ShopRite turns over on Fridays).

| Flag | Env | Default | Purpose |
|---|---|---|---|
| `-zip` | `CIRCULAR_ZIP` | | Postal code that picks your local store circulars |
| `-addr` | `CIRCULAR_ADDR` | `:8080` | Listen address |
| `-data` | `CIRCULAR_DATA` | `data/snapshot.json` | Cached deals and plan |
| `-recipes` | `CIRCULAR_RECIPES` | built-in | Your own recipe catalog (same JSON shape as `internal/recipes/recipes.json`) |
| `-deals-file` | `CIRCULAR_DEALS_FILES` | | Comma-separated JSON deal files, e.g. the Costco coupon book |
| `-offline` | | `false` | Skip Flipp; use only deal files |

`GET /api/plan` returns the current snapshot as JSON.

## Where the deals come from

**ShopRite** publishes its weekly circular (Friday to Thursday) through Flipp.
The app calls Flipp's public item search (`backflipp.wishabi.com`) once per
ingredient keyword in the recipe catalog, keeps results whose merchant is
ShopRite or Costco, and normalizes them into `deals.Deal`. That endpoint is
unofficial, so its shape can change without notice; the parsing lives in one
file (`internal/deals/flipp.go`) to keep that blast radius small.

**Costco** doesn't have a weekly circular. It runs a monthly coupon book
("warehouse savings", roughly four weeks each) and costco.com is guarded by
bot protection, so scraping it directly is brittle. Two ways in:

1. Flipp sometimes carries the Costco coupon book; it's picked up automatically.
2. A JSON deal file (`-deals-file costco.json`) you update once a month, by
   hand or from another tool. See `testdata/sample_deals.json` for the format.

Adding another source means implementing `deals.Source`:

```go
type Source interface {
    Name() string
    Fetch(ctx context.Context, terms []string) ([]Deal, error)
}
```

## How the plan is chosen

Each recipe ingredient lists match terms (and exclusions, so "chicken broth"
doesn't count as chicken). For every recipe, the planner finds the best active
deal per ingredient and scores it by dollar savings (or a flat bonus when only
the sale price is known), weighting the main protein 3x. It then picks the top
recipes with at most two nights per protein, orders them so the same protein
isn't on back-to-back nights, and groups the shopping list by store. The same
week and deals always produce the same plan.

## Layout

```
cmd/circular/        entry point, flags, wiring
internal/deals/      Deal type, Flipp source, JSON file source
internal/recipes/    recipe model and embedded starter catalog
internal/planner/    scoring, selection, shopping list
internal/store/      JSON snapshot cache
internal/web/        HTTP handlers, HTML template, CSS
testdata/            sample deals for offline runs and tests
```
