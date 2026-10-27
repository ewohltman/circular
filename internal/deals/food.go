package deals

import (
	"cmp"
	"slices"
	"strings"
)

// maxFoodPrice is the most a single grocery deal should cost. Food in Flipp's
// results, Costco bulk packs included, stays far below it, while the Apple
// Watches, MacBooks and sofas that a search for "apple" turns up don't.
const maxFoodPrice = 100.0

// foodCategory is the top level for food and drink in the product taxonomy
// that Flipp's categories follow. Tobacco Products is its one inedible branch.
const foodCategory = "Food, Beverages & Tobacco"

// Food reports whether the deal is something to eat or drink: it must be
// priced like groceries and, when its source categorizes it, be filed under
// food or beverages. Uncategorized deals (hand-written files, the odd Flipp
// item) are judged on price alone.
func (d Deal) Food() bool {
	if cmp.Or(d.Price, d.RegularPrice) > maxFoodPrice {
		return false
	}
	if d.Category == "" {
		return true
	}
	top, sub, _ := strings.Cut(d.Category, " > ")
	return top == foodCategory && sub != "Tobacco Products"
}

// FoodOnly removes deals that aren't food, in place.
func FoodOnly(in []Deal) []Deal {
	return slices.DeleteFunc(in, func(d Deal) bool { return !d.Food() })
}
