# Filtering

Two filters decide what reaches the plan: one keeps only food, and the other
matches deals to recipe ingredients.

## Relevant items

Both sources download a whole circular or coupon book (564 ShopRite items,
183 Costco coupons). Each source keeps only items whose name contains a recipe
match term (`deals.Relevant`, case-insensitive). `recipes.SearchTerms` returns
every match term in the catalog, not just the first per ingredient.

For ShopRite this filter also decides which items get OCR, which keeps a
refresh to about 110 tiles. The planner applies each ingredient's exclusions
afterwards.

## Food only

`deals.FoodOnly` drops anything that isn't food before planning
(`internal/deals/food.go`). A deal must:

1. **cost $100 or less.** The sale price is used, or the regular price when
   the sale price is unknown. Real grocery deals stay far below this. Across
   44 recipe searches on Flipp, the most expensive food was $39.99.
2. **be filed under "Food, Beverages & Tobacco"** (and not its Tobacco
   Products branch) when its source gives a category.

Costco coupons come only from the book's Grocery set and are tagged with that
category. ShopRite's circular has no categories, so ShopRite items are judged
on price alone.

The rule replaced an earlier list of Apple product names. Searching Flipp for
"apple" returned AirPods, Apple Watches and MacBooks, and an Apple Watch once
became the "apples" in a pork chop recipe. Price and category catch those,
along with cheaper non-food that names alone don't, such as hand soap and dog
treats.

## Matching deals to ingredients

Each ingredient in `internal/recipes/recipes.json` has `match` terms
(substrings) and `exclude` terms:

```json
{"name": "bell peppers", "match": ["pepper"],
 "exclude": ["black pepper", "pepperoni", "dr pepper", "peppermint", "pepperidge", "jalape"]}
```

Matching on substrings is simple but generous. Checked against the full
October 2026 circular, these false matches were found and excluded:

| Ingredient | False matches |
|---|---|
| bell peppers | Häagen-Dazs Peppermint Bark, Pepperidge Farm, jalapeños |
| steak | Tyson Crispy Chicken **Strip**s, Steak-umm, swordfish steaks |
| chicken breast | Black Bear deli ham, turkey or chicken breast |
| butter | Smart Balance buttery spread, **Butter**ball turkey, Mrs **Butter**worth's |
| apples | Sn**apple**, Pine**apple**s, caramel apples, apple cinnamon sausage |
| Italian sausage | Jimmy Dean sausage patties |
| rolls | Totino's pizza rolls, cinnamon rolls, crescents |
| shrimp | breaded shrimp appetizers, shrimp cocktail |
| potatoes | Pringles potato crisps, instant potatoes |
| rice, pasta | Knorr Pasta or Rice Sides |
| shredded cheese | Pirate's Booty white cheddar popcorn |
| salad greens | Salads of the Sea crab salad and dip |
| mozzarella | mozzarella sticks |

The circular changes every week, so new false matches will turn up. When one
does, add an `exclude` term to the ingredient.
