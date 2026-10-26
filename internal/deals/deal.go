// Package deals fetches and normalizes weekly supermarket deals.
package deals

import (
	"context"
	"strings"
	"time"
)

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
	Price        float64   `json:"price"`                   // sale price; 0 when unknown
	RegularPrice float64   `json:"regular_price,omitempty"` // 0 when unknown
	Unit         string    `json:"unit,omitempty"`          // e.g. "lb", "ea"
	SaleStory    string    `json:"sale_story,omitempty"`    // e.g. "Save $2", "$4 OFF"
	ImageURL     string    `json:"image_url,omitempty"`
	ValidFrom    time.Time `json:"valid_from"`
	ValidTo      time.Time `json:"valid_to"`
	Source       string    `json:"source"`
}

// Savings returns the known dollar discount, or 0 when it can't be determined.
func (d Deal) Savings() float64 {
	if d.RegularPrice > d.Price && d.Price > 0 {
		return d.RegularPrice - d.Price
	}
	return 0
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

// MatchStore maps a free-form merchant name to a supported store.
func MatchStore(merchant string) (Store, bool) {
	m := strings.ToLower(merchant)
	switch {
	case strings.Contains(m, "shoprite"):
		return ShopRite, true
	case strings.Contains(m, "costco"):
		return Costco, true
	}
	return "", false
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
