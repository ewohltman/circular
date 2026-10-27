// Package pricetag reads prices off the printed tags in a store circular with
// the Tesseract OCR engine.
//
// It works on a crop of one product tile. Tesseract reads the tile three ways
// (grayscale, black and white, and scaled down, since the big price digits
// read better smaller while the fine print reads better full size), and a
// value is kept only when the passes that saw it agree. Red "SAVE $X"
// banners are white text on red, which none of those passes read reliably,
// so they are found by color and read on their own. The tile's product name
// must appear in the OCR text: some circular hotspots are misaligned with
// their page images, and a crop showing a neighbor's tile is skipped rather
// than given the neighbor's price. docs/ocr.md has the measurements behind
// these choices.
package pricetag

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"image"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Tag is what a price tag says. Amounts are per unit and 0 when unknown.
type Tag struct {
	Price   float64 // sale price
	Regular float64 // regular price, from "will scan at $X each"
	Save    float64 // dollars off
	Unit    string  // "lb" when priced by the pound
	Story   string  // e.g. "2 for $7, save $5.56 when you buy 2"
}

// ErrNoTitle means the crop doesn't show the expected product.
var ErrNoTitle = errors.New("pricetag: product name not found in the tile")

// maxPrice discards readings no grocery tag would show.
const maxPrice = 100.0

// Reader reads price tags. The zero value runs "tesseract" from PATH.
type Reader struct {
	Tesseract string
}

// Read returns the price tag in tile, which should show the product called
// title. It returns ErrNoTitle when no pass finds the title in the tile.
func (r *Reader) Read(ctx context.Context, tile image.Image, title string) (Tag, error) {
	bin := cmp.Or(r.Tesseract, "tesseract")
	gray := toGray(tile)
	passes := []struct {
		img   *image.Gray
		scale float64
	}{
		{gray, 1},
		{threshold(gray, 170), 1},
		{scale(gray, 0.6), 0.6},
	}
	var results []passResult
	for _, p := range passes {
		lines, err := ocrLines(ctx, bin, p.img, p.scale)
		if err != nil {
			return Tag{}, err
		}
		res, ok := readPass(lines, title)
		if !ok {
			continue
		}
		if leadingSeven(res.price) {
			if res.price, err = checkSeven(ctx, bin, p.img, res.price); err != nil {
				return Tag{}, err
			}
		}
		results = append(results, res)
	}
	if len(results) == 0 {
		return Tag{}, ErrNoTitle
	}
	// A price under the title means the crop runs into the next tile, so only
	// banners within two lines of the title are this tile's.
	limit := tile.Bounds().Dy()
	for _, r := range results {
		if r.belowPrice {
			limit = results[0].titleBottom + 2*results[0].lineHeight
		}
	}
	var banners []banner
	var redPrices []passResult
	for _, box := range redBanners(tile) {
		top := box.Min.Y - tile.Bounds().Min.Y
		if top > limit {
			continue
		}
		b, err := readBanner(ctx, bin, tile, box)
		if err != nil {
			return Tag{}, err
		}
		if b.save > 0 {
			banners = append(banners, b)
			continue
		}
		// Some tiles print the price itself in red; read it as a fallback. Big
		// digits only: a banner whose "SAVE" didn't read would give its amount.
		if top <= results[0].titleBottom && box.Dy() >= 50 {
			text, err := ocrText(ctx, bin, toGray(crop(tile, box)), "7")
			if err != nil {
				return Tag{}, err
			}
			red := passResult{titleBottom: results[0].titleBottom}
			red.readPrices(normalize(text))
			red.lb = red.lb || lbWord.MatchString(normalize(text))
			if red.price > 0 || red.multiN > 0 {
				redPrices = append(redPrices, red)
			}
		}
	}
	return combine(results, banners, redPrices), nil
}

// line is one OCR'd line with its vertical extent in tile pixels.
type line struct {
	text        string
	top, bottom int
}

// passResult is what one OCR pass found, before voting.
type passResult struct {
	price       float64
	multiN      int     // the 2 in "2 for $7"
	multiTotal  float64 // the 7
	regular     float64
	save        float64
	saveLB      bool // the savings are per pound
	lb          bool // the price is per pound
	sawFor      bool // "for" was read above the title, as in "2 FOR $7"
	belowPrice  bool // a price reading sits under the title
	titleBottom int
	lineHeight  int
}

var (
	// The "$" may be followed by the dollars and then superscript cents with
	// no decimal point ("$1⁹⁹" reads as "$199"). A trailing "°" or "%" is a
	// misread superscript, so such readings are dropped by the caller.
	priceRe   = regexp.MustCompile(`\$\s?(\d{1,2})[.,]?(\d{2})`)
	multiRe   = regexp.MustCompile(`(\d{1,2})\s*(?:for)?\s*[$%]\s?(\d{1,2})`)
	scanAtRe  = regexp.MustCompile(`scan\s*at\s*\$\s?(\d{1,2})[.,]?(\d{2})`)
	saveRe    = regexp.MustCompile(`save\s*(?:up\s*to\s*)?\$\s?(\d{1,2})[.,]?(\d{2})(\s*/\s*lb)?`)
	saveCents = regexp.MustCompile(`save\s*(\d{2})\s*[¢c]`)
	buyRe     = regexp.MustCompile(`buy\s*(\d{1,2})`)
	forWord   = regexp.MustCompile(`\bfor\b`)
	lbWord    = regexp.MustCompile(`(?:^|[\s/\d])(?:lb|ib|1b)\b`)
	wordRe    = regexp.MustCompile(`[a-z0-9]+`)
)

// titleWords are the words of a product name that OCR can be checked against.
func titleWords(s string) []string {
	var out []string
	for _, w := range wordRe.FindAllString(normalize(s), -1) {
		if len(w) >= 3 {
			out = append(out, w)
		}
	}
	return out
}

func normalize(s string) string {
	return strings.NewReplacer("’", "'", "‘", "'").Replace(strings.ToLower(s))
}

// readPass parses one pass's lines. It reports false when fewer than half the
// title's words appear, i.e. the crop doesn't show this product. Prices count
// only when they sit above or level with the title: tiles print the price
// over or beside the name, so a price below it belongs to the next tile.
func readPass(lines []line, title string) (passResult, bool) {
	want := titleWords(title)
	if len(want) == 0 {
		return passResult{}, false
	}
	found := map[string]bool{}
	var res passResult
	for _, l := range lines {
		hit := false
		for _, w := range titleWords(l.text) {
			for _, t := range want {
				if w == t {
					found[w], hit = true, true
				}
			}
		}
		if hit {
			res.titleBottom = max(res.titleBottom, l.bottom)
			res.lineHeight = max(res.lineHeight, l.bottom-l.top)
		}
	}
	if 2*len(found) < len(want) {
		return passResult{}, false
	}
	for _, l := range lines {
		t := normalize(l.text)
		if m := scanAtRe.FindStringSubmatch(t); m != nil {
			res.regular = cmp.Or(res.regular, money(m[1], m[2]))
			continue
		}
		if amount, perLB, ok := parseSave(t); ok {
			if l.top <= res.titleBottom+2*res.lineHeight {
				res.save = cmp.Or(res.save, amount)
				res.saveLB = res.saveLB || perLB
			}
			continue
		}
		if l.top > res.titleBottom {
			var below passResult
			below.readPrices(t)
			res.belowPrice = res.belowPrice || below.price > 0 || below.multiN > 0
			continue
		}
		res.readPrices(t)
		res.sawFor = res.sawFor || forWord.MatchString(t)
		if !titleLine(l.text, want) && lbWord.MatchString(t) {
			res.lb = true // "lb." printed beside the price, often as its own word
		}
	}
	return res, true
}

func titleLine(text string, want []string) bool {
	for _, w := range titleWords(text) {
		if slices.Contains(want, w) {
			return true
		}
	}
	return false
}

// leadingSeven reports whether a price's dollars start with 7.
func leadingSeven(price float64) bool {
	return price >= 7 && price < 8 || price >= 70 && price < 80
}

// checkSeven fixes the circular typeface's most common misread. Its "1" has a
// flag that Tesseract reads as "7" ("$1⁹⁹" as "$799"), often in every pass.
// The glyphs differ in width, so the digit after the "$" is measured: the
// "1" is about 0.43 times as wide as it is tall, other digits about 0.72.
func checkSeven(ctx context.Context, bin string, img image.Image, price float64) (float64, error) {
	boxes, err := tesseract(ctx, bin, img, "11", "makebox")
	if err != nil || !narrowSeven(boxes) {
		return price, err
	}
	if price < 8 {
		return round(price - 6), nil
	}
	return round(price - 60), nil
}

// narrowSeven reports whether the first large "7" after a "$" in Tesseract's
// box output is too narrow to be a 7. Rows are "char left bottom right top".
func narrowSeven(boxes string) bool {
	rows := strings.Split(boxes, "\n")
	for i := 0; i+1 < len(rows); i++ {
		dollar, next := strings.Fields(rows[i]), strings.Fields(rows[i+1])
		if len(dollar) < 5 || dollar[0] != "$" || len(next) < 5 || next[0] != "7" {
			continue
		}
		x0, _ := strconv.Atoi(next[1])
		y0, _ := strconv.Atoi(next[2])
		x1, _ := strconv.Atoi(next[3])
		y1, _ := strconv.Atoi(next[4])
		if h := y1 - y0; h >= 20 { // skip fine print; the price digits are big
			return float64(x1-x0) < 0.55*float64(h)
		}
	}
	return false
}

// readPrices takes the first single or multi-buy price on a line.
func (res *passResult) readPrices(t string) {
	if res.multiN == 0 {
		for _, m := range multiRe.FindAllStringSubmatchIndex(t, -1) {
			if m[0] > 0 && (isDigit(t[m[0]-1]) || t[m[0]-1] == '$') || misread(t, m[1], ".,°") {
				continue
			}
			n, _ := strconv.Atoi(t[m[2]:m[3]])
			total, _ := strconv.ParseFloat(t[m[4]:m[5]], 64)
			if n >= 2 && n <= 10 && total > 0 {
				res.multiN, res.multiTotal = n, total
				break
			}
		}
	}
	if res.price == 0 {
		for _, m := range priceRe.FindAllStringSubmatchIndex(t, -1) {
			if misread(t, m[1], "%°") {
				continue
			}
			res.price = money(t[m[2]:m[3]], t[m[4]:m[5]])
			break
		}
	}
}

// misread reports whether the reading ending at i runs into another digit
// or one of the given characters, which means OCR merged or garbled it.
func misread(t string, i int, also string) bool {
	if i >= len(t) {
		return false
	}
	return isDigit(t[i]) || strings.ContainsRune(also, rune(t[i])) || strings.HasPrefix(t[i:], "°")
}

func isDigit(c byte) bool { return '0' <= c && c <= '9' }

// parseSave reads "SAVE $5.56", "SAVE UP TO $2.55", "SAVE $2.00/LB" or
// "SAVE 50¢".
func parseSave(t string) (amount float64, perLB, ok bool) {
	if m := saveRe.FindStringSubmatch(t); m != nil {
		return money(m[1], m[2]), m[3] != "", true
	}
	if m := saveCents.FindStringSubmatch(t); m != nil {
		c, _ := strconv.Atoi(m[1])
		return float64(c) / 100, false, true
	}
	return 0, false, false
}

func money(dollars, cents string) float64 {
	d, _ := strconv.Atoi(dollars)
	c, _ := strconv.Atoi(cents)
	return round(float64(d) + float64(c)/100)
}

// banner is a red "SAVE" banner and the "WHEN YOU BUY N" box beside it.
type banner struct {
	save  float64
	perLB bool
	buy   int
}

// combine votes on each value across passes.
func combine(results []passResult, banners []banner, redPrices []passResult) Tag {
	var tag Tag
	price := vote(results, func(r passResult) float64 { return r.price }, 1)
	multiN := vote(results, func(r passResult) float64 { return float64(r.multiN) }, 1)
	multiTotal := vote(results, func(r passResult) float64 { return r.multiTotal }, 1)
	// Multi-buys are 2 to 5 for $M, or a "for" was read; otherwise a stray
	// digit beside a price ("8 $29" for "$2⁹⁹", the 8 being an icon) can pass
	// for one.
	if multiN > 5 && !slices.ContainsFunc(results, func(r passResult) bool { return r.sawFor }) {
		multiN = 0
	}
	if price > 0 {
		multiN = 0 // a single price wins over a multi-buy reading beside it
	}
	lb := slices.ContainsFunc(results, func(r passResult) bool { return r.lb })
	if price == 0 && multiN == 0 && len(redPrices) > 0 {
		// Only when no pass read a price: red badges such as "ONLY $4.79/lb"
		// would otherwise outvote the real price.
		price = vote(redPrices, func(r passResult) float64 { return r.price }, 1)
		multiN = vote(redPrices, func(r passResult) float64 { return float64(r.multiN) }, 1)
		multiTotal = vote(redPrices, func(r passResult) float64 { return r.multiTotal }, 1)
		lb = slices.ContainsFunc(redPrices, func(r passResult) bool { return r.lb })
	}
	tag.Regular = vote(results, func(r passResult) float64 { return r.regular }, 1)
	save := vote(results, func(r passResult) float64 { return r.save }, 1)
	buy := 0
	perLB := slices.ContainsFunc(results, func(r passResult) bool { return r.saveLB && r.save == save })
	for _, b := range banners {
		if b.save > 0 {
			save, perLB, buy = b.save, b.perLB, b.buy // banners read more reliably than passes
			break
		}
	}
	var story []string
	switch {
	case multiN >= 2 && multiTotal > 0:
		tag.Price = round(multiTotal / multiN)
		story = append(story, fmt.Sprintf("%d for %s", int(multiN), dollars(multiTotal)))
	case price > 0 && price <= maxPrice:
		tag.Price = price
		if lb || perLB {
			tag.Unit = "lb"
		}
		story = append(story, dollars(price)+unitSuffix(tag.Unit))
	}
	if perLB {
		tag.Unit = "lb"
	}
	if save > 0 && save <= maxPrice {
		n := cmp.Or(buy, int(multiN), 1)
		tag.Save = round(save / float64(n))
		s := "save " + dollars(save) + unitSuffix(tag.Unit)
		if buy > 1 {
			s += fmt.Sprintf(" when you buy %d", buy)
		}
		story = append(story, s)
	}
	if tag.Regular > 0 && tag.Regular <= tag.Price {
		tag.Regular = 0
	}
	if tag.Regular > 0 {
		story = append(story, "regularly "+dollars(tag.Regular))
	}
	tag.Story = strings.Join(story, ", ")
	return tag
}

// vote returns the value at least need passes agree on, or 0. Conflicting
// readings need two votes to settle regardless.
func vote(results []passResult, get func(passResult) float64, need int) float64 {
	counts := map[float64]int{}
	for _, r := range results {
		if v := get(r); v > 0 {
			counts[v]++
		}
	}
	var best float64
	bestN := 0
	for v, n := range counts {
		if n > bestN || (n == bestN && v < best) {
			best, bestN = v, n
		}
	}
	if bestN >= need && (len(counts) == 1 || bestN >= 2) {
		return best
	}
	return 0
}

func round(v float64) float64 { return math.Round(v*100) / 100 }

func dollars(v float64) string {
	if v == math.Trunc(v) {
		return fmt.Sprintf("$%d", int(v))
	}
	return fmt.Sprintf("$%.2f", v)
}

func unitSuffix(unit string) string {
	if unit == "" {
		return ""
	}
	return "/" + unit
}
