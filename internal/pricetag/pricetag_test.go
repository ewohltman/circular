package pricetag

import (
	"errors"
	"image"
	"image/color"
	"image/draw"
	"os/exec"
	"testing"
)

// Line text below is what Tesseract returned for real ShopRite tiles,
// misreads included.

func TestReadPass(t *testing.T) {
	for _, tc := range []struct {
		name  string
		title string
		lines []line
		ok    bool
		want  passResult
	}{
		{
			name:  "superscript cents",
			title: "Bowl & Basket Apple Cider",
			lines: []line{{"$599", 10, 60}, {"LIMIT 4 PER VARIETY", 70, 80}, {"Bowl & Basket", 85, 100}, {"Apple Cider", 102, 118}},
			ok:    true,
			want:  passResult{price: 5.99},
		},
		{
			name:  "multi-buy without the FOR",
			title: "Totino’s Ultimate & Original Pizza Rolls",
			lines: []line{{"= 2$7", 9, 56}, {"Totino’s Ul", 63, 77}, {"Original Pizza Rolls", 83, 100}, {"SAVE $5.56", 161, 175}},
			ok:    true,
			want:  passResult{multiN: 2, multiTotal: 7},
		},
		{
			name:  "regular price and per-pound unit",
			title: "Hass Avocados",
			lines: []line{{"$1399", 30, 80}, {"Ib.", 67, 79}, {"Hass", 85, 100}, {"Avocados", 101, 116}, {"willscan at $1.09 each.", 130, 140}},
			ok:    true,
			want:  passResult{price: 13.99, regular: 1.09, lb: true},
		},
		{
			name:  "garbled superscript is dropped",
			title: "Hanger Steaks",
			lines: []line{{"= $139°", 32, 80}, {"Hanger", 91, 107}, {"Steaks", 111, 125}},
			ok:    true,
			want:  passResult{},
		},
		{
			name:  "price under the title belongs to the next tile",
			title: "Butterball 93% Ground Turkey",
			lines: []line{{"Butterball 93%", 0, 15}, {"Ground Turkey", 16, 30}, {"$1598", 60, 110}, {"2-lb. Bag Bowl & Basket Shrimp", 120, 135}},
			ok:    true,
			want:  passResult{belowPrice: true},
		},
		{
			name:  "crop shows another product",
			title: "Pompeian Olive Oil",
			lines: []line{{"8$8", 10, 50}, {"Annie’s Mac & Cheese", 60, 80}},
			ok:    false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := readPass(tc.lines, tc.title)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			got.titleBottom, got.lineHeight = 0, 0
			if got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestCombine(t *testing.T) {
	for _, tc := range []struct {
		name    string
		passes  []passResult
		banners []banner
		red     []passResult
		want    Tag
	}{
		{
			name:    "multi-buy savings are per unit",
			passes:  []passResult{{multiN: 2, multiTotal: 7}, {multiN: 2, multiTotal: 7}, {}},
			banners: []banner{{save: 5.56, buy: 2}},
			want:    Tag{Price: 3.5, Save: 2.78, Story: "2 for $7, save $5.56 when you buy 2"},
		},
		{
			name:   "conflicting single readings give no price",
			passes: []passResult{{price: 7.99}, {price: 1.99}, {price: 71.99}},
			want:   Tag{},
		},
		{
			name:   "a single price beats a multi-buy misread",
			passes: []passResult{{price: 2.99}, {multiN: 8, multiTotal: 29}},
			want:   Tag{Price: 2.99, Story: "$2.99"},
		},
		{
			name:   "an unlikely multi-buy without FOR is dropped",
			passes: []passResult{{multiN: 8, multiTotal: 29}},
			want:   Tag{},
		},
		{
			name:    "per-pound savings set the unit",
			passes:  []passResult{{price: 9.99}},
			banners: []banner{{save: 2, perLB: true}},
			want:    Tag{Price: 9.99, Save: 2, Unit: "lb", Story: "$9.99/lb, save $2/lb"},
		},
		{
			name:   "red price is only a fallback",
			passes: []passResult{{price: 23.95}},
			red:    []passResult{{price: 4.79, lb: true}},
			want:   Tag{Price: 23.95, Story: "$23.95"},
		},
		{
			name:   "red price used when nothing else read",
			passes: []passResult{{}},
			red:    []passResult{{price: 1.99, lb: true}},
			want:   Tag{Price: 1.99, Unit: "lb", Story: "$1.99/lb"},
		},
		{
			name:   "regular price at or under the sale price is ignored",
			passes: []passResult{{price: 2.99, regular: 2.99}},
			want:   Tag{Price: 2.99, Story: "$2.99"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := combine(tc.passes, tc.banners, tc.red); got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseSave(t *testing.T) {
	for text, want := range map[string]struct {
		amount float64
		perLB  bool
	}{
		"'save $5.56 [yl":     {5.56, false},
		"; save$2.00":         {2, false},
		"save up to $2.55 ]":  {2.55, false},
		"save $2.00/lb.":      {2, true},
		"2, saves0¢ limit 4":  {0, false},
		"save 50¢":            {0.5, false},
		"limit 4 per variety": {0, false},
	} {
		amount, perLB, _ := parseSave(text)
		if amount != want.amount || perLB != want.perLB {
			t.Errorf("parseSave(%q) = %v, %v; want %v, %v", text, amount, perLB, want.amount, want.perLB)
		}
	}
}

func TestNarrowSeven(t *testing.T) {
	// Tesseract makebox rows: char left bottom right top page.
	one := "$ 10 20 30 70 0\n7 32 20 51 64 0\n9 53 40 70 64 0\n"   // "$1⁹⁹" read as "$799"
	seven := "$ 10 20 30 70 0\n7 32 20 70 73 0\n9 72 40 90 73 0\n" // a real $7.99
	small := "$ 10 20 14 30 0\n7 15 20 18 30 0\n"                  // fine print
	if !narrowSeven(one) {
		t.Errorf("narrow 1 not caught")
	}
	if narrowSeven(seven) || narrowSeven(small) {
		t.Errorf("real or small 7 flagged")
	}
	if got := leadingSeven(7.99) && leadingSeven(71.99) && !leadingSeven(17.99); !got {
		t.Errorf("leadingSeven wrong")
	}
}

func TestParseTSV(t *testing.T) {
	tsv := "level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext\n" +
		"5\t1\t2\t1\t1\t1\t10\t60\t40\t20\t90\tApple\n" +
		"5\t1\t2\t1\t1\t2\t55\t62\t40\t18\t90\tCider\n" +
		"5\t1\t1\t1\t1\t1\t10\t6\t80\t48\t70\t$599\n" +
		"4\t1\t1\t1\t1\t0\t10\t6\t80\t48\t-1\t\n"
	got := parseTSV(tsv, 0.5)
	want := []line{{"$599", 12, 108}, {"Apple Cider", 120, 160}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("parseTSV = %+v, want %+v", got, want)
	}
}

func TestRedBanners(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 300, 200))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	red := image.NewUniform(color.RGBA{220, 30, 40, 255})
	draw.Draw(img, image.Rect(20, 120, 200, 150), red, image.Point{}, draw.Src) // banner
	draw.Draw(img, image.Rect(220, 20, 260, 60), red, image.Point{}, draw.Src)  // square logo
	got := redBanners(img.SubImage(image.Rect(0, 100, 300, 200)))
	if len(got) != 1 || !got[0].Overlaps(image.Rect(20, 120, 200, 150)) {
		t.Errorf("redBanners = %v, want one banner near (20,120)-(200,150)", got)
	}
}

func TestReadWithTesseract(t *testing.T) {
	if _, err := exec.LookPath("tesseract"); err != nil {
		t.Skip("tesseract not installed")
	}
	// A blank tile has no title, so it must be rejected rather than priced.
	img := image.NewGray(image.Rect(0, 0, 200, 120))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	if _, err := (&Reader{}).Read(t.Context(), img, "Hass Avocados"); !errors.Is(err, ErrNoTitle) {
		t.Errorf("err = %v, want ErrNoTitle", err)
	}
}
