package deals

import "testing"

func TestFood(t *testing.T) {
	const (
		food  = "Food, Beverages & Tobacco > Food Items"
		drink = "Food, Beverages & Tobacco > Beverages"
	)
	// Names and categories as Flipp returned them for recipe search terms.
	for _, tc := range []struct {
		deal Deal
		want bool
	}{
		{Deal{Name: "Wellsley Farms Organic Honeycrisp Apples 4 Lbs.", Category: food}, true},
		{Deal{Name: "Mott's Apple Juice", Price: 7.49, Category: drink}, true},
		{Deal{Name: "ORGANIC FARM Organic Brown Sweet Rice 15 LB", Price: 39.99, Category: food}, true},
		{Deal{Name: "Kirkland Signature Wild Alaskan Sockeye Salmon, 3 lb", Price: 32.99, RegularPrice: 38.99}, true},
		{Deal{Name: "Apple Watch Series 11 (GPS + Cellular) 42mm", Price: 349.97, Category: "Apparel & Accessories > Jewelry"}, false},
		{Deal{Name: "AirPods 4 with Active Noise Cancellation", Price: 119.97, Category: "Electronics > Audio"}, false},
		{Deal{Name: "Apple - EarPods (USB-C) - White", Price: 19.99, Category: "Electronics > Audio"}, false},
		{Deal{Name: "Winter Candy Apple Gentle & Clean Foaming Hand Soap", Price: 7.95, Category: "Health & Beauty"}, false},
		{Deal{Name: "Blue Buffalo Health Bars Apple & Yogurt Dog Treats", Price: 4.98, Category: "Animals & Pet Supplies > Pet Supplies"}, false},
		{Deal{Name: "Henredon Murphy Fabric 6-piece Modular Sectional", Price: 2199.99, Category: "Furniture > Sofas"}, false},
		{Deal{Name: "Cigarettes, carton", Price: 80, Category: "Food, Beverages & Tobacco > Tobacco Products"}, false},
		// Uncategorized: price decides, falling back to the regular price.
		{Deal{Name: "Apple MacBook Air Laptop (13-inch)", Price: 1259.99}, false},
		{Deal{Name: `Apple iPad 11" A16 128GB`, RegularPrice: 449.99}, false},
	} {
		if got := tc.deal.Food(); got != tc.want {
			t.Errorf("Food(%q, $%.2f, %q) = %v, want %v", tc.deal.Name, tc.deal.Price, tc.deal.Category, got, tc.want)
		}
	}
}
