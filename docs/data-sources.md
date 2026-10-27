# Data sources

Findings from October 2026. Store numbers, catalogue IDs and counts are
examples from that week and will change.

## Costco: the warehouse savings page

Costco has no weekly circular. It runs a coupon book of roughly four weeks
("Member-Only Warehouse Savings"), published at
<https://www.costco.com/o/-/warehouse-savings> and linked from the home page.

**Access.** A plain GET with the app's own User-Agent returns HTTP 200 and the
full page (about 3.6 MB). There is no bot challenge on this page, and
`robots.txt` doesn't disallow it (it blocks account, checkout and legacy
search paths). The coupon book is national, so no store or postal code is
needed.

**Format.** The page is a Next.js App Router page rendered on the server. The
framework ships its data as "flight" chunks:

```html
<script>self.__next_f.push([1,"…JSON string…"])</script>
```

Concatenating the chunk strings gives the flight text, a row format of the
framework's own. Each coupon set appears in it as a plain JSON object:

```json
{"couponSetId":"Grocery","uniqueId":"blt…","couponCards":[{
  "cardDescription":"Swanson Less Sodium Chicken Broth",
  "itemNumber":"2071668",
  "validDatesOverride":"Limit 5.",
  "applicability":"warehouse_online",
  "imageUrl":"https://gdx-assets.costco.com/…",
  "pricesOffCSObject":[{"prepend_text":"Save","value":["2.50"],"append_text":""}]
}, …]}
```

That book had 16 sets and 183 coupons: Grocery (31), Pharmacy (47),
Appliances (20), Apparel (17), Household Items (18) and others. The parser
(`internal/deals/costco.go`) finds each `{"couponSetId":` and reads exactly
one JSON value from there.

| Field | Meaning |
|---|---|
| `cardDescription` | product name |
| `itemNumber` | Costco item number; sometimes a list ("2043159, 2077776") |
| `applicability` | `warehouse_only`, `warehouse_online` or `online_only` (the app skips online-only) |
| `validDatesOverride` | despite the name, purchase limits ("Limit 10.") |
| `pricesOffCSObject` | the price, in one of the formats below |

Price formats seen across all 183 coupons:

| Format | Example | Count | Read as |
|---|---|---|---|
| `"Save"` + amount | Save `["4.60"]` | 112 | discount only; the shelf price isn't published |
| amount + `"After $X OFF"` | `["29.99"]` After $20 OFF | 58 | sale price 29.99, regular 49.99 |
| bare amount | `["3,299.99"]` | 5 | price |
| two amounts | `["300", "500"]` | 8 | a range; the app uses the low end |
| no price object | Poppi Prebiotic Soda | 1 | no price |

Amounts can carry spaces and thousands separators (`" 1,800"`).

**Dates.** The book's dates are only in a header text block:
`Valid 10/19/26 - 11/15/26`. A stale `Valid 10/23/24-11/17/24` also lingers
in a disabled component, so the parser takes the range that contains today,
or else the one ending last.

**What's missing.** The shelf price for "Save $X" coupons, so those deals
have a discount but no price. The book is mostly packaged goods. In this one,
nothing fresh matched a dinner recipe once false matches were excluded (see
[filtering](filtering.md)).

**If it breaks.** The coupon data is also rendered as HTML with stable
`data-testid` hooks (`Text_prices_and_percentages_prices`,
`Text_prices_and_percentages_append_text`) if the flight format changes.
Costco's own APIs on `gdx-api.costco.com` appear in the page but were not
needed or tested.

## ShopRite: the weekly circular from Red Pepper

shoprite.com's circular page (`/sm/pickup/rsid/{rsid}/circulars`) embeds an
iframe from Red Pepper Digital, ShopRite's circular vendor:

```
https://app.redpepper.digital/a10/publications/home/4573?StoreId=…
```

`4573` is ShopRite's account. The viewer is an Angular app whose data comes
from a JSON API at `https://node.redpepper.digital`, a Drupal REST backend
(`?_format=json`). It is served by nginx with no bot protection and needs no
key. `robots.txt` on `app.redpepper.digital` allows everything; the API host
has none.

### Endpoints used

All are GET requests with no authentication.

**`/client/4573/catalogue/geo_location/json?_format=json`** lists 572 stores,
one row per store and circular:

```json
{"field_store_id":"524","field_store_name":"ShopRite of North Brunswick",
 "field_contact_address":"400 Renaissance Blvd","field_zipcode":"08902",
 "field_dma":"R1 JULY 2026_New","field_version":"16067065", …}
```

`field_version` is the ID of a circular that store carries. Store 524 had two
rows: the weekly circular and a "Wellness Your Way" pharmacy flyer.

**`/client/4573/catalogues/json?_format=json`** lists the account's 28 live
circulars. Most were regional editions of "Week of 10/25":

```json
{"nid_1":"16067065","title":"Week of 10/25","share":"week-of-10-25-z1-r1",
 "start":"2026-10-22T00:01","finish":"2026-10-31T23:59", …}
```

Times are Eastern with no zone, and come both with and without seconds. The
circular goes live on Thursday for preview, but the sale runs Sunday to
Saturday. Promo codes elsewhere in the API are dated `10_25_2026_…`, so the
app takes the sale start from the "Week of 10/25" title.

**`/catalogue/{id}/page-images/json?_format=json`** lists the pages (17) and
their images on CloudFront, e.g. a 1425×3188 JPEG. Pages vary in size: page 4
was 1147×3186.

**`/rpms_catalogue/{id}/page/0/{pages}/regions`** returns the clickable
regions, keyed by page number:

```json
{"id":"17085271","field_region_type":"product","page":"1",
 "field_product_title":"Friendly’s Single Serve Sundaes",
 "field_product_description":"6-fl. oz. pkg., Any Variety",
 "field_product_id":"00029839000517,00029839000258,…",
 "coords":"6.75,67.56,1000001,28.87,9.96",
 "field_product_image_url":["https://…png"], "promo_code_ids":[209382]}
```

The circular had 564 products. `coords` is left, top, z-order, width and
height, as percentages of the page image. `field_product_id` lists the UPCs
the deal covers.

### No prices in the data

Every region has price fields (`field_product_price`,
`field_product_discount_price`, `region_price` and others), and all of them
were empty. These were also checked, and none has prices:

| Endpoint | Returns |
|---|---|
| `…/regions/alllivepricing`, `…/regions/allquickview`, `/region/{id}/price/json` | `[]` |
| `…/productsnew` | the same 564 items without coordinates |
| `/client/4573/master/skus` | 24,628 promo code to UPC rows |
| `/accessibility/4573/data` | screen reader labels for the viewer's buttons |

The prices exist only as printed text on the page images, so the app reads
them with OCR ([details](ocr.md)).

### Hotspots don't always line up

Region coordinates were last edited on 10/20, and the page images were
revised later (`…_21Oct2026_BP_23Oct2026_SR_004.jpeg`). On page 4 the right
column's hotspots sit about one tile too low, so cropping Butterball ground
turkey's region shows the shrimp below it. The OCR step checks that the crop
shows the product's name before trusting any price in it.

## Ruled out

### shoprite.com's store API

shoprite.com (Wakefern's site, operated by Wynshop, with assets served from
Mi9's cloud storage) knows each store's real prices. A product search page embeds results in
`window.__PRELOADED_STATE__`:

```json
{"name":"Perdue 98% Fat Free Boneless Skinless Thin Sliced Chicken Breasts",
 "sku":"…","price":"$3.89 avg/ea","wasPrice":"$7.79 avg/ea",
 "priceSource":"tpr","priceLabel":"On Sale!","isDiscounted":true,"promotions":[]}
```

`priceSource` is `tpr` (temporary price reduction) for sales and `regular`
otherwise. There are no validity dates.

Every non-browser request to `www.`, `api.`, `shop.` or
`storefrontgateway.shoprite.com` gets HTTP 403 with `cf-mitigated: challenge`,
even `robots.txt`. Only a real browser, which runs Cloudflare's JavaScript
check, gets through. Using this from the app would mean defeating that bot
protection, which is what it exists to stop, and would break whenever
Cloudflare changes. The app doesn't do it.

### Flipp

The app first searched Flipp's public item search
(`backflipp.wishabi.com/flipp/items/search`), an aggregator. Findings:

- ShopRite isn't on Flipp. Flipp's flyer listing for 08902 had 72 flyers from
  47 merchants, and for 07052 it had 83 flyers from 53 merchants, with no
  ShopRite in either. A search for "shoprite" returned nothing.
- Costco is there with two flyers: general merchandise (where Apple products
  and furniture came from) and a grocery flyer of packaged goods. The grocery
  flyer covers the same coupon book the app now reads from costco.com.
- Search results are ranked and trimmed. A plain "chicken" search returned
  102 items and missed Costco's chicken broth, which a "swanson" search found.

The Flipp source was removed.

## Terms of use

Notes, not legal advice.

- **ShopRite** ([terms](https://www.shoprite.com/sm/pickup/rsid/3000/website-terms-and-conditions),
  operated for Wakefern): the terms let you view and print site material for
  non-commercial use. They also say that no material may be "transcribed,
  stored in a retrieval system" without Wakefern's permission. A personal
  planner that stores a week of circular data is a gray area under that.
  The circular is fetched from Red Pepper rather than shoprite.com, but it is
  still Wakefern's content.
- **Costco** ([terms](https://www.costco.com/f/-/terms-and-conditions-of-use)):
  no clause about automated access was found in the site terms. They do
  assert ownership of site materials.

The app identifies itself honestly (`User-Agent: circular-meal-planner/…`) and
makes about 20 requests per refresh: one Costco page, two Red Pepper store and
catalogue lists, two calls per circular, and the page images that have
relevant items (about a dozen). OCR results are cached for the life of the
process, so a second refresh in the same week skips the page images.
