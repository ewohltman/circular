# circular

A small Go web app that pulls this week's ShopRite and Costco deals and builds
a seven-night dinner plan and shopping list around them.

## Run it

Requires Go 1.27 or newer, and [Tesseract](docs/ocr.md#setup) for ShopRite
prices.

```sh
# Live data for ShopRite of North Brunswick (store 524) and Costco
go run ./cmd/circular -shoprite-store 524

# Or pick the ShopRite store by postal code
go run ./cmd/circular -zip 08902

# Offline demo with the bundled sample deals
go run ./cmd/circular -offline -deals-file testdata/sample_deals.json
```

Open http://localhost:8080 and press **Refresh deals**. The app also refreshes
on its own when a new circular week starts. ShopRite's prices run Sunday to
Saturday, so plans do too.

| Flag | Env | Default | Purpose |
|---|---|---|---|
| `-shoprite-store` | `CIRCULAR_SHOPRITE_STORE` | | ShopRite store number, e.g. `524` |
| `-zip` | `CIRCULAR_ZIP` | | Postal code; picks the ShopRite store there when no store number is set |
| `-tesseract` | `CIRCULAR_TESSERACT` | `tesseract` | Tesseract binary for reading ShopRite prices; empty skips prices |
| `-addr` | `CIRCULAR_ADDR` | `:8080` | Listen address |
| `-data` | `CIRCULAR_DATA` | `data/snapshot.json` | Cached deals and plan |
| `-recipes` | `CIRCULAR_RECIPES` | built-in | Your own recipe catalog (same JSON shape as `internal/recipes/recipes.json`) |
| `-deals-file` | `CIRCULAR_DEALS_FILES` | | Comma-separated JSON deal files for anything the sources miss |
| `-offline` | | `false` | Skip the live sources; use only deal files |

`GET /api/plan` returns the current snapshot as JSON.

## Where the deals come from

The full findings are in [docs/data-sources.md](docs/data-sources.md).

**Costco** (`internal/deals/costco.go`) reads the monthly coupon book from
costco.com's warehouse savings page. The page is rendered on the server and
embeds every coupon as JSON. The app keeps the Grocery coupons that are
redeemable in the warehouse. Many give only a discount ("Save $4.60"), not a
shelf price.

**ShopRite** (`internal/deals/shoprite.go`) reads the store's weekly circular
from Red Pepper Digital, the vendor shoprite.com embeds it from. Its JSON API
lists every item with its place on the page image, but no prices, so the app
crops each relevant item's tile from the page and reads the price tag with
Tesseract (`internal/pricetag`). About half the tiles yield a price. The rest
stay "on sale, price unknown" rather than risk a misread. See
[docs/ocr.md](docs/ocr.md) for how it works and how accurate it is.

**Deal files** (`-deals-file`) are JSON arrays in the shape of
`testdata/sample_deals.json`, for deals no source covers. Keys are
case-sensitive. `category` is optional, e.g.
`"Food, Beverages & Tobacco > Food Items"`.

Adding another source means implementing `deals.Source`:

```go
type Source interface {
    Name() string
    Fetch(ctx context.Context, terms []string) ([]Deal, error)
}
```

## How the plan is chosen

Each recipe ingredient lists match terms (and exclusions, so "chicken broth"
doesn't count as chicken). Sources keep only items matching some term, and
only food is kept: a deal must cost $100 or less and, when its source
categorizes it, be filed under food or beverages
([docs/filtering.md](docs/filtering.md)). For every recipe, the planner finds
the best active deal per ingredient and scores it by dollar savings (or a flat
bonus when only the sale price is known), weighting the main protein 3x. It
then picks the top recipes with at most two nights per protein, orders them so
the same protein isn't on back-to-back nights, and groups the shopping list by
store. The same week and deals always produce the same plan.

## Layout

```
cmd/circular/        entry point, flags, wiring
internal/deals/      Deal type, Costco and ShopRite sources, JSON file source
internal/pricetag/   price tag OCR with Tesseract
internal/recipes/    recipe model and embedded starter catalog
internal/planner/    scoring, selection, shopping list
internal/store/      JSON snapshot cache
internal/web/        HTTP handlers, HTML template, CSS
testdata/            sample deals for offline runs and tests
docs/                findings and design notes
```

## Development

CI (`.github/workflows/ci.yml`) runs on every push to `main` and every pull
request. The `Makefile` runs the same checks locally (`make help` lists every
target):

```sh
make ci     # tidy check, build, vet, race tests, golangci-lint
make test   # just the tests
make fmt    # apply the gofmt/goimports formatting that lint enforces
make demo   # serve the offline demo on :8080
```

Tests don't need Tesseract; the one test that runs it is skipped when it isn't
installed.

`make lint` runs the golangci-lint version pinned in CI through `go run`, so
the first run downloads and builds it. Set `GOLANGCI_LINT=golangci-lint` to use
a binary you already have.
