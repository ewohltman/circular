// Package deals fetches and normalizes weekly supermarket deals.
package deals

import (
	"context"
	"math"
	"strings"
	"time"
	_ "time/tzdata" // circular dates are Eastern time wherever the app runs
)

// eastern is the time zone both stores publish their dates in.
var eastern = func() *time.Location {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		panic(err) // unreachable with time/tzdata embedded
	}
	return loc
}()

// Store identifies a supported retailer.
type Store string

const (
	ShopRite Store = "ShopRite"
	Costco   Store = "Costco"
)

// Deal is a single sale item, normalized across sources.
type Deal struct {
	ID           string    `json:"id"`
	Store        Store     `json:"store"`
	Name         string    `json:"name"`
	Price        float64   `json:"price"`                  // sale price per unit; 0 when unknown
	RegularPrice float64   `json:"regular_price,omitzero"` // 0 when unknown
	Discount     float64   `json:"discount,omitzero"`      // dollars off per unit when prices aren't both known, e.g. Costco's "Save $4.60"
	Unit         string    `json:"unit,omitempty"`         // e.g. "lb", "ea"
	SaleStory    string    `json:"sale_story,omitempty"`   // e.g. "2 for $7, save $5.56", "$4 OFF"
	Category     string    `json:"category,omitempty"`     // e.g. "Food, Beverages & Tobacco > Food Items"
	ImageURL     string    `json:"image_url,omitempty"`
	ValidFrom    time.Time `json:"valid_from"`
	ValidTo      time.Time `json:"valid_to"`
	Source       string    `json:"source"`
}

// Savings returns the known dollar discount per unit, or 0 when it can't be
// determined.
func (d Deal) Savings() float64 {
	if d.RegularPrice > d.Price && d.Price > 0 {
		return d.RegularPrice - d.Price
	}
	return d.Discount
}

// Overlaps reports whether the deal is valid at any point in [from, to).
// Zero bounds are treated as open.
func (d Deal) Overlaps(from, to time.Time) bool {
	if !d.ValidFrom.IsZero() && !d.ValidFrom.Before(to) {
		return false
	}
	if !d.ValidTo.IsZero() && d.ValidTo.Before(from) {
		return false
	}
	return true
}

// Source produces deals for one or more stores.
type Source interface {
	Name() string
	// Fetch returns deals relevant to the given search terms. Sources that
	// can't search (e.g. a static file) may ignore terms and return everything.
	Fetch(ctx context.Context, terms []string) ([]Deal, error)
}

// Relevant reports whether name contains any of terms, ignoring case. It is
// how sources that download a whole circular keep only what the recipes can
// use; the planner applies each ingredient's exclusions afterwards.
func Relevant(name string, terms []string) bool {
	n := strings.ToLower(name)
	for _, t := range terms {
		if strings.Contains(n, t) {
			return true
		}
	}
	return false
}

// Dedupe removes deals with duplicate IDs, keeping the first.
func Dedupe(in []Deal) []Deal {
	seen := make(map[string]bool, len(in))
	out := in[:0]
	for _, d := range in {
		key := string(d.Store) + "|" + d.ID
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, d)
	}
	return out
}

// cents rounds a dollar amount to the nearest cent.
func cents(v float64) float64 { return math.Round(v*100) / 100 }
