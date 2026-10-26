package deals

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// DefaultFlippURL is the public search endpoint behind flipp.com. It is
// unofficial and undocumented, so treat its shape as subject to change.
const DefaultFlippURL = "https://backflipp.wishabi.com/flipp/items/search"

// Flipp searches weekly-ad items via Flipp, which aggregates the ShopRite
// circular and (when published) Costco's coupon book.
type Flipp struct {
	BaseURL    string
	PostalCode string
	Stores     []Store
	Client     *http.Client
	// Delay between requests to stay polite.
	Delay time.Duration
}

func (f *Flipp) Name() string { return "flipp" }

type flippResponse struct {
	Items []flippItem `json:"items"`
}

type flippItem struct {
	ID            json.Number `json:"flyer_item_id"`
	Name          string      `json:"name"`
	MerchantName  string      `json:"merchant_name"`
	CurrentPrice  json.Number `json:"current_price"`
	OriginalPrice json.Number `json:"original_price"`
	PostPriceText string      `json:"post_price_text"`
	SaleStory     string      `json:"sale_story"`
	ImageURL      string      `json:"clean_image_url"`
	ValidFrom     time.Time   `json:"valid_from"`
	ValidTo       time.Time   `json:"valid_to"`
}

func (f *Flipp) Fetch(ctx context.Context, terms []string) ([]Deal, error) {
	base := f.BaseURL
	if base == "" {
		base = DefaultFlippURL
	}
	client := f.Client
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	want := make(map[Store]bool, len(f.Stores))
	for _, s := range f.Stores {
		want[s] = true
	}

	var out []Deal
	for i, term := range terms {
		if i > 0 && f.Delay > 0 {
			select {
			case <-ctx.Done():
				return out, ctx.Err()
			case <-time.After(f.Delay):
			}
		}
		items, err := f.search(ctx, client, base, term)
		if err != nil {
			return out, fmt.Errorf("flipp search %q: %w", term, err)
		}
		for _, it := range items {
			store, ok := MatchStore(it.MerchantName)
			if !ok || (len(want) > 0 && !want[store]) {
				continue
			}
			out = append(out, Deal{
				ID:           it.ID.String(),
				Store:        store,
				Name:         it.Name,
				Price:        parseNum(it.CurrentPrice),
				RegularPrice: parseNum(it.OriginalPrice),
				Unit:         it.PostPriceText,
				SaleStory:    it.SaleStory,
				ImageURL:     it.ImageURL,
				ValidFrom:    it.ValidFrom,
				ValidTo:      it.ValidTo,
				Source:       f.Name(),
			})
		}
	}
	return Dedupe(out), nil
}

func (f *Flipp) search(ctx context.Context, client *http.Client, base, term string) ([]flippItem, error) {
	q := url.Values{}
	q.Set("locale", "en-us")
	q.Set("postal_code", f.PostalCode)
	q.Set("q", term)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "circular-meal-planner/0.1")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %s", resp.Status)
	}
	var body flippResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return body.Items, nil
}

func parseNum(n json.Number) float64 {
	if n == "" {
		return 0
	}
	v, err := strconv.ParseFloat(string(n), 64)
	if err != nil {
		return 0
	}
	return v
}
