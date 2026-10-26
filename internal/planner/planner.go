// Package planner builds a weekly dinner plan that leans on current deals.
package planner

import (
	"cmp"
	"hash/fnv"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/ewohltman/circular/internal/deals"
	"github.com/ewohltman/circular/internal/recipes"
)

// Scoring weights. A deal with a known discount scores its dollar savings;
// otherwise it scores a flat bonus so "on sale, amount unknown" still counts.
const (
	mainWeight     = 3.0
	sideWeight     = 1.0
	unknownSavings = 1.0
	maxPerProtein  = 2
	defaultDays    = 7
)

// IngredientMatch pairs a recipe ingredient with the best deal found, if any.
type IngredientMatch struct {
	Ingredient recipes.Ingredient `json:"ingredient"`
	Deal       *deals.Deal        `json:"deal,omitempty"`
}

// Meal is one scheduled dinner.
type Meal struct {
	Day     string            `json:"day"`
	Date    time.Time         `json:"date"`
	Recipe  recipes.Recipe    `json:"recipe"`
	Matches []IngredientMatch `json:"matches"`
	Score   float64           `json:"score"`
}

// OnSale counts how many of the meal's ingredients have a deal.
func (m Meal) OnSale() int {
	n := 0
	for _, im := range m.Matches {
		if im.Deal != nil {
			n++
		}
	}
	return n
}

// ShoppingItem is one line of the shopping list.
type ShoppingItem struct {
	Name  string      `json:"name"`
	Deal  *deals.Deal `json:"deal,omitempty"`
	Meals []string    `json:"meals"`
}

// Plan is a week of meals plus the derived shopping list.
type Plan struct {
	WeekOf  time.Time                      `json:"week_of"`
	Meals   []Meal                         `json:"meals"`
	ByStore map[deals.Store][]ShoppingItem `json:"by_store"`
	NoDeal  []ShoppingItem                 `json:"no_deal"`
	Savings float64                        `json:"savings"` // known savings only
}

// Options tunes plan generation.
type Options struct {
	Days int // defaults to 7
}

// Build scores every recipe against the active deals and picks a varied week.
// weekOf is the first day of the plan; it also seeds tie-breaking so the same
// week and deals always produce the same plan.
func Build(rs []recipes.Recipe, ds []deals.Deal, weekOf time.Time, opt Options) Plan {
	days := opt.Days
	if days <= 0 {
		days = defaultDays
	}
	// Compare by overlap rather than a single instant so a deal that starts a
	// few hours into the week (e.g. a circular published in another timezone)
	// still counts.
	weekEnd := weekOf.AddDate(0, 0, days)
	active := make([]deals.Deal, 0, len(ds))
	for _, d := range ds {
		if d.Overlaps(weekOf, weekEnd) {
			active = append(active, d)
		}
	}

	type scored struct {
		meal Meal
		jit  float64
	}
	rng := rand.New(rand.NewPCG(seed(weekOf), 0))
	cands := make([]scored, 0, len(rs))
	for _, r := range rs {
		m := scoreRecipe(r, active)
		cands = append(cands, scored{meal: m, jit: rng.Float64()})
	}
	slices.SortStableFunc(cands, func(a, b scored) int {
		return cmp.Or(cmp.Compare(b.meal.Score, a.meal.Score), cmp.Compare(b.jit, a.jit))
	})

	// Greedy pick with a cap per protein; relax the cap if the catalog is
	// too small to fill the week.
	var picked []Meal
	used := map[string]bool{}
	for limit := maxPerProtein; len(picked) < days && limit <= days; limit++ {
		perProtein := map[string]int{}
		for _, m := range picked {
			perProtein[m.Recipe.Protein]++
		}
		for _, c := range cands {
			if len(picked) == days {
				break
			}
			if used[c.meal.Recipe.ID] || perProtein[c.meal.Recipe.Protein] >= limit {
				continue
			}
			used[c.meal.Recipe.ID] = true
			perProtein[c.meal.Recipe.Protein]++
			picked = append(picked, c.meal)
		}
		if len(used) == len(cands) {
			break
		}
	}

	// Spread proteins out so the same one isn't on back-to-back nights.
	picked = interleave(picked)
	for i := range picked {
		d := weekOf.AddDate(0, 0, i)
		picked[i].Date = d
		picked[i].Day = d.Weekday().String()
	}

	p := Plan{WeekOf: weekOf, Meals: picked, ByStore: map[deals.Store][]ShoppingItem{}}
	buildShoppingList(&p)
	return p
}

func scoreRecipe(r recipes.Recipe, ds []deals.Deal) Meal {
	m := Meal{Recipe: r}
	for _, in := range r.Ingredients {
		im := IngredientMatch{Ingredient: in}
		best := -1.0
		for i := range ds {
			if !in.Matches(ds[i].Name) {
				continue
			}
			v := ds[i].Savings()
			if v == 0 {
				v = unknownSavings
			}
			if v > best {
				best = v
				im.Deal = new(ds[i])
			}
		}
		if im.Deal != nil {
			w := sideWeight
			if in.Main {
				w = mainWeight
			}
			m.Score += w * best
		}
		m.Matches = append(m.Matches, im)
	}
	return m
}

// interleave reorders meals so adjacent nights differ in protein when possible.
func interleave(ms []Meal) []Meal {
	out := make([]Meal, 0, len(ms))
	rest := slices.Clone(ms)
	for len(rest) > 0 {
		idx := 0
		if len(out) > 0 {
			last := out[len(out)-1].Recipe.Protein
			for i, m := range rest {
				if m.Recipe.Protein != last {
					idx = i
					break
				}
			}
		}
		out = append(out, rest[idx])
		rest = slices.Delete(rest, idx, idx+1)
	}
	return out
}

func buildShoppingList(p *Plan) {
	type key struct {
		store deals.Store
		id    string
	}
	seenDeal := map[key]int{}
	seenPlain := map[string]int{}
	for _, m := range p.Meals {
		for _, im := range m.Matches {
			if im.Deal == nil {
				if i, ok := seenPlain[im.Ingredient.Name]; ok {
					p.NoDeal[i].Meals = append(p.NoDeal[i].Meals, m.Recipe.Name)
					continue
				}
				seenPlain[im.Ingredient.Name] = len(p.NoDeal)
				p.NoDeal = append(p.NoDeal, ShoppingItem{Name: im.Ingredient.Name, Meals: []string{m.Recipe.Name}})
				continue
			}
			k := key{im.Deal.Store, im.Deal.ID}
			if i, ok := seenDeal[k]; ok {
				p.ByStore[k.store][i].Meals = append(p.ByStore[k.store][i].Meals, m.Recipe.Name)
				continue
			}
			seenDeal[k] = len(p.ByStore[k.store])
			p.ByStore[k.store] = append(p.ByStore[k.store], ShoppingItem{Name: im.Ingredient.Name, Deal: im.Deal, Meals: []string{m.Recipe.Name}})
			p.Savings += im.Deal.Savings()
		}
	}
}

func seed(t time.Time) uint64 {
	h := fnv.New64a()
	h.Write([]byte(t.Format("2006-01-02")))
	return h.Sum64()
}

// WeekStart returns midnight of the most recent Friday on or before t. ShopRite
// circulars currently run Friday through Thursday.
func WeekStart(t time.Time) time.Time {
	t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	offset := (int(t.Weekday()) - int(time.Friday) + 7) % 7
	return t.AddDate(0, 0, -offset)
}
