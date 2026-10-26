package planner

import (
	"testing"
	"time"

	"github.com/ewohltman/circular/internal/deals"
	"github.com/ewohltman/circular/internal/recipes"
)

func TestBuildPrefersDealsAndVariety(t *testing.T) {
	rs, err := recipes.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	week := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	end := week.AddDate(0, 0, 7)
	ds := []deals.Deal{
		{ID: "1", Store: deals.ShopRite, Name: "Perdue Boneless Chicken Breast", Price: 1.99, RegularPrice: 4.99, ValidFrom: week, ValidTo: end},
		{ID: "2", Store: deals.ShopRite, Name: "Bowl & Basket Chicken Thighs", Price: 1.49, RegularPrice: 2.99, ValidFrom: week, ValidTo: end},
		{ID: "3", Store: deals.Costco, Name: "Kirkland Wild Salmon Fillets", Price: 29.99, RegularPrice: 35.99, ValidFrom: week, ValidTo: end},
		{ID: "4", Store: deals.ShopRite, Name: "Expired Ground Beef", Price: 1, RegularPrice: 10, ValidTo: week.AddDate(0, 0, -1)},
		{ID: "6", Store: deals.ShopRite, Name: "Russet Potatoes", Price: 2, RegularPrice: 4, ValidFrom: week.Add(4 * time.Hour), ValidTo: end},
		{ID: "5", Store: deals.ShopRite, Name: "Chicken Broth", Price: 1, RegularPrice: 3, ValidFrom: week, ValidTo: end},
	}

	p := Build(rs, ds, week, Options{})
	if len(p.Meals) != 7 {
		t.Fatalf("want 7 meals, got %d", len(p.Meals))
	}

	ids := map[string]bool{}
	perProtein := map[string]int{}
	for i, m := range p.Meals {
		if ids[m.Recipe.ID] {
			t.Errorf("recipe %s repeated", m.Recipe.ID)
		}
		ids[m.Recipe.ID] = true
		perProtein[m.Recipe.Protein]++
		if i > 0 && p.Meals[i-1].Recipe.Protein == m.Recipe.Protein && m.Recipe.Protein != "vegetarian" {
			// Allowed only when unavoidable; with this catalog it is avoidable.
			t.Errorf("back-to-back %s on day %d", m.Recipe.Protein, i)
		}
		for _, im := range m.Matches {
			if im.Deal != nil && im.Deal.ID == "4" {
				t.Errorf("expired deal used in %s", m.Recipe.ID)
			}
		}
	}
	for prot, n := range perProtein {
		if n > maxPerProtein {
			t.Errorf("%s appears %d times", prot, n)
		}
	}
	for _, want := range []string{"chicken-stir-fry", "sheet-pan-chicken", "baked-salmon"} {
		if !ids[want] {
			t.Errorf("expected deal-backed recipe %s in plan", want)
		}
	}
	if !hasDeal(p, "6") {
		t.Errorf("deal starting hours into the week was ignored")
	}
	if len(p.ByStore[deals.ShopRite]) == 0 || len(p.ByStore[deals.Costco]) == 0 {
		t.Errorf("shopping list missing a store: %+v", p.ByStore)
	}
	if p.Savings < 10 {
		t.Errorf("savings = %.2f, want >= 10", p.Savings)
	}

	again := Build(rs, ds, week, Options{})
	for i := range p.Meals {
		if p.Meals[i].Recipe.ID != again.Meals[i].Recipe.ID {
			t.Fatalf("plan not deterministic at day %d", i)
		}
	}
}

func TestWeekStart(t *testing.T) {
	mon := time.Date(2026, 10, 26, 15, 0, 0, 0, time.UTC)
	got := WeekStart(mon)
	if got.Weekday() != time.Friday || got.Day() != 23 {
		t.Errorf("WeekStart(%v) = %v", mon, got)
	}
}

func hasDeal(p Plan, id string) bool {
	for _, m := range p.Meals {
		for _, im := range m.Matches {
			if im.Deal != nil && im.Deal.ID == id {
				return true
			}
		}
	}
	return false
}
