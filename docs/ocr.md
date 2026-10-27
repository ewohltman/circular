# Reading prices with OCR

ShopRite's circular data has every item but no prices ([why](data-sources.md#no-prices-in-the-data)).
Prices are printed on the page images, so `internal/pricetag` reads them with
[Tesseract](https://github.com/tesseract-ocr/tesseract), a free OCR engine run
as a command (`tesseract stdin stdout …`). No cgo or API key is needed.

## Setup

Install Tesseract with English data:

```sh
sudo pacman -S tesseract tesseract-data-eng   # Arch / CachyOS
sudo apt install tesseract-ocr                # Debian / Ubuntu
brew install tesseract                        # macOS
```

The app looks for `tesseract` on `PATH`, or at `-tesseract /path/to/binary`.
If it isn't found, the app logs a warning and ShopRite deals come without
prices. The planner then gives them a flat "on sale" bonus. Tests don't need
Tesseract; one test runs it when it's installed.

## What a tile looks like

Each circular item is a tile: product photo, name, size text, and a price in
one of several styles.

| Style | Example | Notes |
|---|---|---|
| single price, superscript cents | $5⁹⁹ | OCR reads "$599", with no decimal point |
| per pound | $2⁹⁹ lb. | "lb." is small and often missed |
| multi-buy | 2 FOR $7 | "FOR" is often missed, leaving "2$7" |
| savings banner | SAVE $5.56 WHEN YOU BUY 2 | white on red |
| regular price | Less or additional items will scan at $1.09 each | fine print |
| red price | $1⁹⁹ lb. in red | some produce and meat tiles |

## The pipeline

For each relevant item (one matching a recipe term; about 110 of the 564):

1. **Crop** the item's region from its page image, using Red Pepper's
   percentage coordinates.
2. **Run three OCR passes** in sparse-text mode (`--psm 11`, TSV output, so
   each line has a position): grayscale, black and white (threshold 170), and
   grayscale scaled to 60%. The large price digits often read better smaller,
   and the fine print reads better at full size.
3. **Check the title.** A pass counts only if at least half the words (of
   three or more letters) of the product's name appear in it. If none
   qualifies, the crop shows something else (see misaligned hotspots below)
   and the item keeps an unknown price.
4. **Read the price from above the title only.** Tiles print the price above
   or beside the name, never below it. A price under the name belongs to the
   next tile, and marks the crop as running into it.
5. **Vote.** A value is kept when every pass that saw it agrees, or when two
   passes agree. A disagreement such as $7.99, $1.99 and $71.99 yields no
   price rather than a guess.
6. **Read savings banners separately.** Red regions at least 2.5 times as wide
   as tall are found by color, cropped, inverted, enlarged 2×, and read as one
   line (`--psm 7`). The white "WHEN YOU BUY N" box to their right gives N, so
   savings are per unit.
7. **Fall back to red prices.** If no pass found a price, large red regions
   (50 px or taller) are read as price text. Only as a fallback: badges like
   "ONLY $4.79/lb" would otherwise outvote the real price.

Tiles run in parallel, one per CPU, with `OMP_THREAD_LIMIT=1` for each
Tesseract process. A full refresh read about 110 tiles in 6 seconds on the
development machine.

## Failure modes found, and the fix for each

All of these came from comparing OCR output with the actual tiles.

| Problem | Example | Fix |
|---|---|---|
| Hotspot misaligned with its page | Butterball's crop showed the shrimp tile | Title check (step 3); price only from above the title (step 4) |
| Superscript cents garbled | "$13⁹⁹" read as "$139°" (= $1.39) | A price followed by `°` or `%` is discarded |
| "1" read as "7" | "$1⁹⁹" read as "$799" in all three passes | See below |
| Icon read as a digit | lock icon + "$2⁹⁹" read as "8 $29" (8 for $29) | A single price beats a multi-buy reading; multi-buys over 5 need "FOR" to be read |
| Banner unreadable in normal passes | "SAVE $5.56" missed | Banners are found by color and read on their own (step 6) |
| Banner read as a price | "SAVE $1.30" read as "$1350" | The red-price fallback only reads regions at least 50 px tall |

**The "1" in this typeface** has a flag that Tesseract reads as "7",
consistently and in every pass. Requiring two passes to agree didn't help: it
lost eight correct prices and kept the wrong ones. The two glyphs differ in
width, though. Measured from Tesseract's character boxes (`makebox`), the
"1" is 0.42–0.44 times as wide as it is tall, while "7", "2" and "5" are
0.71–0.73. When a price starts with 7, the reader measures that glyph and
changes the digit to 1 if the ratio is under 0.55. This corrected all six such
misreads that were checked, and left a real $7.99 alone.

## Accuracy

In the October 2026 live run, the ShopRite source returned 114 relevant items.
Of those, 62 got a price, 29 got a known saving, and 71 got at least one of
the two.

About 60 priced tiles were compared by eye with their images after the fixes
above, and every price was correct. Known gaps:

- **Recall over guessing.** Items whose passes disagree, or whose crop is
  misaligned, get no price. The planner still counts them as on sale.
- **Units.** "lb" is detected on only some per-pound tiles. One false "/lb"
  was seen: "20 LB" printed on a rice bag in the product photo.
- **Savings.** "SAVE UP TO $X" and "SAVE 60¢" banners are sometimes missed.
- **Typeface-specific.** The thresholds (0.55 glyph ratio, 50 px red text,
  170 gray level) were tuned on ShopRite's current circular design. A
  redesign may need them re-measured.

## Re-measuring

To check accuracy after a change, run the ShopRite source and compare its
results with the tiles. Red Pepper's page images and coordinates give you the
crops (see [data sources](data-sources.md#endpoints-used)). Keep any crops out
of the repository: they are ShopRite's copyrighted circular.
