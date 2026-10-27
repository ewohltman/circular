# Design notes

How circular finds its deals and why it works the way it does. These notes
record what was found while building the app (October 2026), so the reasoning
survives when a site changes and something needs fixing.

| Page | What it covers |
|---|---|
| [Data sources](data-sources.md) | Where ShopRite and Costco publish deals, what each endpoint returns, what was ruled out, and the sites' terms |
| [Reading prices with OCR](ocr.md) | How ShopRite prices are read off the circular's page images with Tesseract, and how accurate that is |
| [Filtering](filtering.md) | Which deals count as food and how deals are matched to recipe ingredients |
| [encoding/json/v2](json-v2.md) | Why the code uses `encoding/json/v2` and what changed in the migration |

## Summary

- **Costco**: the monthly coupon book on costco.com
  (`/o/-/warehouse-savings`) is rendered on the server and embeds every
  coupon as JSON. A plain GET works. Only the Grocery set is used.
- **ShopRite**: the weekly circular on shoprite.com is embedded from Red
  Pepper Digital, ShopRite's circular vendor. Its JSON API lists each store's
  circular items with their position on the page, but no prices. Prices are
  read from the page images with Tesseract OCR.
- **Ruled out**: shoprite.com's own store API has exact prices but sits
  behind Cloudflare's bot challenge for anything but a real browser. Flipp,
  the aggregator the app first used, doesn't carry ShopRite at all.
