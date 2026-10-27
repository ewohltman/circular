package deals

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DefaultCostcoURL is costco.com's member-only warehouse savings page: the
// monthly coupon book, roughly four weeks per book.
const DefaultCostcoURL = "https://www.costco.com/o/-/warehouse-savings"

// costcoFood is the category given to coupons from the book's Grocery set.
const costcoFood = "Food, Beverages & Tobacco"

// userAgent identifies the app to the sites it reads.
const userAgent = "circular-meal-planner/0.2"

// CostcoCoupons reads the coupon book from costco.com. The page is rendered on the
// server by Next.js, and each coupon set is embedded in the page as JSON
// inside the framework's flight data (self.__next_f.push calls). Only the
// Grocery set is kept, and only coupons redeemable in the warehouse.
type CostcoCoupons struct {
	URL    string
	Client *http.Client
	Now    func() time.Time
}

func (c *CostcoCoupons) Name() string { return "costco.com" }

type costcoSet struct {
	ID    string       `json:"couponSetId"`
	Cards []costcoCard `json:"couponCards"`
}

type costcoCard struct {
	Description   string        `json:"cardDescription"`
	ItemNumber    string        `json:"itemNumber"`
	Limits        string        `json:"validDatesOverride"` // e.g. "Limit 5."
	Applicability string        `json:"applicability"`      // warehouse_only, warehouse_online or online_only
	ImageURL      string        `json:"imageUrl"`
	Prices        []costcoPrice `json:"pricesOffCSObject"`
}

type costcoPrice struct {
	Prepend string   `json:"prepend_text"` // "Save" for discount-only coupons
	Values  []string `json:"value"`        // e.g. ["39.99"], [" 1,800"], ["300", "500"]
	Append  string   `json:"append_text"`  // e.g. "After $10 OFF"
}

func (c *CostcoCoupons) Fetch(ctx context.Context, terms []string) ([]Deal, error) {
	page, err := c.get(ctx)
	if err != nil {
		return nil, err
	}
	flight, err := nextFlight(page)
	if err != nil {
		return nil, err
	}
	sets, err := costcoSets(flight)
	if err != nil {
		return nil, err
	}
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	from, to, ok := costcoValidity(flight, now())
	if !ok {
		return nil, errors.New("costco: no coupon book dates on the page")
	}
	var out []Deal
	for _, s := range sets {
		if s.ID != "Grocery" {
			continue
		}
		for i, card := range s.Cards {
			if card.Applicability == "online_only" || !Relevant(card.Description, terms) {
				continue
			}
			d := Deal{
				ID:        cmp.Or(card.ItemNumber, fmt.Sprintf("grocery-%d", i)),
				Store:     Costco,
				Name:      card.Description,
				Category:  costcoFood,
				ImageURL:  card.ImageURL,
				ValidFrom: from,
				ValidTo:   to,
				Source:    c.Name(),
			}
			if len(card.Prices) > 0 {
				costcoPrices(&d, card.Prices[0])
			}
			if l := strings.TrimSpace(card.Limits); l != "" {
				d.SaleStory = strings.TrimPrefix(d.SaleStory+" · "+l, " · ")
			}
			out = append(out, d)
		}
	}
	return out, nil
}

func (c *CostcoCoupons) get(ctx context.Context) (string, error) {
	u := cmp.Or(c.URL, DefaultCostcoURL)
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("costco: unexpected status %s", resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

const flightPush = "self.__next_f.push("

// nextFlight reassembles the Next.js flight text from a server-rendered page.
// Each chunk is a call like self.__next_f.push([1,"<JSON string>"]); the
// argument is a JSON array, so the decoder reads it and stops at its end.
func nextFlight(page string) (string, error) {
	var b strings.Builder
	for rest := page; ; {
		i := strings.Index(rest, flightPush)
		if i < 0 {
			break
		}
		rest = rest[i+len(flightPush):]
		v, err := jsontext.NewDecoder(strings.NewReader(rest)).ReadValue()
		if err != nil {
			continue // not a literal array, e.g. a push of a variable
		}
		var chunk []any
		if err := json.Unmarshal(v, &chunk); err != nil || len(chunk) != 2 {
			continue
		}
		if s, ok := chunk[1].(string); ok {
			b.WriteString(s)
		}
		rest = rest[len(v):]
	}
	if b.Len() == 0 {
		return "", errors.New("costco: page has no Next.js flight data")
	}
	return b.String(), nil
}

const couponSetStart = `{"couponSetId":`

// costcoSets decodes every coupon set object embedded in the flight text.
// The objects sit inside the framework's own (non-JSON) row format, so each
// one is read as a single JSON value starting at its opening brace.
func costcoSets(flight string) ([]costcoSet, error) {
	var sets []costcoSet
	for rest := flight; ; {
		i := strings.Index(rest, couponSetStart)
		if i < 0 {
			break
		}
		rest = rest[i:]
		v, err := jsontext.NewDecoder(strings.NewReader(rest)).ReadValue()
		if err != nil {
			return nil, fmt.Errorf("costco: read coupon set: %w", err)
		}
		var s costcoSet
		if err := json.Unmarshal(v, &s); err != nil {
			return nil, fmt.Errorf("costco: decode coupon set: %w", err)
		}
		sets = append(sets, s)
		rest = rest[len(v):]
	}
	if len(sets) == 0 {
		return nil, errors.New("costco: no coupon sets on the page")
	}
	return sets, nil
}

var costcoDates = regexp.MustCompile(`Valid (\d{1,2}/\d{1,2}/\d{2}) ?- ?(\d{1,2}/\d{1,2}/\d{2})`)

// costcoValidity finds the coupon book's "Valid 10/19/26 - 11/15/26" header.
// Stale ranges linger in disabled page components, so it prefers the range
// containing now and otherwise the one that ends last.
func costcoValidity(flight string, now time.Time) (from, to time.Time, ok bool) {
	for _, m := range costcoDates.FindAllStringSubmatch(flight, -1) {
		f, err1 := time.ParseInLocation("1/2/06", m[1], eastern)
		t, err2 := time.ParseInLocation("1/2/06", m[2], eastern)
		if err1 != nil || err2 != nil {
			continue
		}
		t = t.Add(24*time.Hour - time.Second) // through the end of the last day
		if !now.Before(f) && !now.After(t) {
			return f, t, true
		}
		if !ok || t.After(to) {
			from, to, ok = f, t, true
		}
	}
	return from, to, ok
}

var dollarsOff = regexp.MustCompile(`\$\s*([\d,]+(?:\.\d+)?)\s*OFF`)

// costcoPrices fills in prices from one of the book's price formats:
// "Save" plus an amount (discount only), an amount plus "After $X OFF"
// (sale price and discount), or a bare amount. Ranges use the low end.
func costcoPrices(d *Deal, p costcoPrice) {
	var amount float64
	for i, v := range p.Values {
		f, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(v), ",", ""), 64)
		if err == nil && (i == 0 || f < amount) {
			amount = f
		}
	}
	if amount == 0 {
		return
	}
	switch {
	case strings.EqualFold(strings.TrimSpace(p.Prepend), "save"):
		d.Discount = amount
		d.SaleStory = fmt.Sprintf("Save $%.2f", amount)
	case dollarsOff.MatchString(p.Append):
		off, _ := strconv.ParseFloat(strings.ReplaceAll(dollarsOff.FindStringSubmatch(p.Append)[1], ",", ""), 64)
		d.Price = amount
		d.RegularPrice = cents(amount + off)
		d.SaleStory = fmt.Sprintf("$%.2f after $%.2f off", amount, off)
	default:
		d.Price = amount
	}
}
