package deals

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // circular pages are JPEGs
	"maps"
	"net/http"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ewohltman/circular/internal/pricetag"
)

// DefaultRedPepperURL is the API behind ShopRite's weekly circular.
// shoprite.com embeds the circular from Red Pepper Digital, its circular
// vendor; the API needs no key and isn't behind shoprite.com's bot wall.
const DefaultRedPepperURL = "https://node.redpepper.digital"

// shopRiteClient is ShopRite's account with Red Pepper.
const shopRiteClient = "4573"

// TagReader reads the price tag in one circular tile. *pricetag.Reader
// implements it with Tesseract.
type TagReader interface {
	Read(ctx context.Context, tile image.Image, title string) (pricetag.Tag, error)
}

// ShopRiteCircular reads a store's weekly circular from Red Pepper. Items come
// with names, UPCs and their position on the page image, but no prices: those
// are only printed on the page, so they are read from each item's tile with
// Tags. Only items matching the search terms are read.
type ShopRiteCircular struct {
	BaseURL    string
	Store      string // ShopRite store number, e.g. "524"; found by PostalCode when empty
	PostalCode string
	Tags       TagReader // nil leaves prices unknown
	Workers    int       // tiles read at once; defaults to the CPU count
	Client     *http.Client
	Now        func() time.Time

	mu    sync.Mutex
	cache map[string]tagResult // by catalogue and region, so refreshes skip OCR
}

type tagResult struct {
	tag pricetag.Tag
	ok  bool
}

func (s *ShopRiteCircular) Name() string { return "redpepper" }

type rpStore struct {
	Number  string `json:"field_store_id"`
	Name    string `json:"field_store_name"`
	Zip     string `json:"field_zipcode"`
	Version string `json:"field_version"` // the ID of a catalogue this store carries
}

type rpCatalogue struct {
	ID     string    `json:"nid_1"`
	Title  string    `json:"title"` // e.g. "Week of 10/25"
	Start  time.Time `json:"start"`
	Finish time.Time `json:"finish"`
}

type rpPage struct {
	Page  string `json:"page"`
	Image string `json:"image"`
}

type rpRegion struct {
	ID     string   `json:"id"`
	Type   string   `json:"field_region_type"` // "product" or "link"
	Title  string   `json:"field_product_title"`
	Page   string   `json:"page"`
	Coords string   `json:"coords"` // left, top, z-order, width, height as percentages of the page
	Images []string `json:"field_product_image_url"`
}

// rpTimes parses Red Pepper's local timestamps, which come with and without
// seconds and no zone; ShopRite's are Eastern.
var rpTimes = json.WithUnmarshalers(json.UnmarshalFunc(func(b []byte, t *time.Time) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	if s == "" {
		*t = time.Time{}
		return nil
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04"} {
		if v, err := time.ParseInLocation(layout, s, eastern); err == nil {
			*t = v
			return nil
		}
	}
	return fmt.Errorf("unrecognized time %q", s)
}))

func (s *ShopRiteCircular) Fetch(ctx context.Context, terms []string) ([]Deal, error) {
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	cats, err := s.catalogues(ctx, now)
	if err != nil {
		return nil, err
	}
	var out []Deal
	var errs []error
	for _, c := range cats {
		ds, err := s.catalogue(ctx, c, terms)
		out = append(out, ds...)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", c.Title, err))
		}
	}
	return out, errors.Join(errs...)
}

// catalogues returns the store's circulars that haven't ended yet.
func (s *ShopRiteCircular) catalogues(ctx context.Context, now time.Time) ([]rpCatalogue, error) {
	var stores []rpStore
	if err := s.getJSON(ctx, "/client/"+shopRiteClient+"/catalogue/geo_location/json?_format=json", &stores); err != nil {
		return nil, err
	}
	number := s.Store
	if number == "" {
		var err error
		if number, err = storeByZip(stores, s.PostalCode); err != nil {
			return nil, err
		}
	}
	versions := map[string]bool{}
	for _, st := range stores {
		if st.Number == number {
			versions[st.Version] = true
		}
	}
	if len(versions) == 0 {
		return nil, fmt.Errorf("shoprite: no circulars for store %s", number)
	}
	var all []rpCatalogue
	if err := s.getJSON(ctx, "/client/"+shopRiteClient+"/catalogues/json?_format=json", &all, rpTimes); err != nil {
		return nil, err
	}
	var out []rpCatalogue
	for _, c := range all {
		if versions[c.ID] && (c.Finish.IsZero() || c.Finish.After(now)) {
			out = append(out, c)
		}
	}
	return out, nil
}

// storeByZip picks the lowest-numbered store in a postal code.
func storeByZip(stores []rpStore, zip string) (string, error) {
	var numbers []int
	for _, st := range stores {
		if n, err := strconv.Atoi(st.Number); err == nil && zip != "" && st.Zip == zip {
			numbers = append(numbers, n)
		}
	}
	if len(numbers) == 0 {
		return "", fmt.Errorf("shoprite: no store in postal code %q; set the store number", zip)
	}
	return strconv.Itoa(slices.Min(numbers)), nil
}

// catalogue returns the relevant items from one circular, priced where the
// tag could be read.
func (s *ShopRiteCircular) catalogue(ctx context.Context, c rpCatalogue, terms []string) ([]Deal, error) {
	var pages []rpPage
	if err := s.getJSON(ctx, "/catalogue/"+c.ID+"/page-images/json?_format=json", &pages); err != nil {
		return nil, err
	}
	var regions map[string][]rpRegion
	if err := s.getJSON(ctx, fmt.Sprintf("/rpms_catalogue/%s/page/0/%d/regions", c.ID, len(pages)), &regions); err != nil {
		return nil, err
	}
	from, to := saleWeek(c), c.Finish
	var deals []Deal
	var tiles []tile
	byNumber := func(a, b string) int {
		x, _ := strconv.Atoi(a)
		y, _ := strconv.Atoi(b)
		return cmp.Compare(x, y)
	}
	for _, page := range slices.SortedFunc(maps.Keys(regions), byNumber) {
		for _, r := range regions[page] {
			if r.Type != "product" || !Relevant(r.Title, terms) {
				continue
			}
			d := Deal{
				ID:        r.ID,
				Store:     ShopRite,
				Name:      r.Title,
				ValidFrom: from,
				ValidTo:   to,
				Source:    s.Name(),
			}
			if len(r.Images) > 0 {
				d.ImageURL = r.Images[0]
			}
			deals = append(deals, d)
			tiles = append(tiles, tile{deal: len(deals) - 1, page: r.Page, coords: r.Coords, key: c.ID + "/" + r.ID})
		}
	}
	if s.Tags == nil || len(tiles) == 0 {
		return deals, nil
	}
	err := s.readTags(ctx, pages, tiles, deals)
	return deals, err
}

var weekOf = regexp.MustCompile(`(?i)week of (\d{1,2})/(\d{1,2})`)

// saleWeek is when the circular's prices start. Catalogues go live a few days
// early for preview, so a "Week of 10/25" title wins over the start time.
func saleWeek(c rpCatalogue) time.Time {
	m := weekOf.FindStringSubmatch(c.Title)
	if m == nil || c.Start.IsZero() {
		return c.Start
	}
	month, _ := strconv.Atoi(m[1])
	day, _ := strconv.Atoi(m[2])
	year := c.Start.Year()
	if month < int(c.Start.Month())-6 { // a January week published in December
		year++
	}
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, eastern)
}

// tile is one item whose price tag needs reading.
type tile struct {
	deal   int // index into the deals slice
	page   string
	coords string
	key    string
}

// readTags OCRs the tiles in parallel and fills in prices. Tiles whose crop
// doesn't show the item (misaligned hotspots) keep unknown prices.
func (s *ShopRiteCircular) readTags(ctx context.Context, pages []rpPage, tiles []tile, deals []Deal) error {
	images := map[string]image.Image{}
	for _, t := range tiles {
		if _, ok := images[t.page]; ok || s.cached(t.key) {
			continue
		}
		i := slices.IndexFunc(pages, func(p rpPage) bool { return p.Page == t.page })
		if i < 0 {
			continue
		}
		img, err := s.getImage(ctx, pages[i].Image)
		if err != nil {
			return err
		}
		images[t.page] = img
	}

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		failed int
		first  error
	)
	sem := make(chan struct{}, cmp.Or(s.Workers, runtime.NumCPU()))
	for _, t := range tiles {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			res, err := s.readTile(ctx, images[t.page], t, deals[t.deal].Name)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed++
				first = cmp.Or(first, err)
				return
			}
			if res.ok {
				applyTag(&deals[t.deal], res.tag)
			}
		})
	}
	wg.Wait()
	if failed > 0 {
		return fmt.Errorf("prices unread for %d of %d items: %w", failed, len(tiles), first)
	}
	return nil
}

func (s *ShopRiteCircular) cached(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.cache[key]
	return ok
}

func (s *ShopRiteCircular) readTile(ctx context.Context, page image.Image, t tile, title string) (tagResult, error) {
	s.mu.Lock()
	res, ok := s.cache[t.key]
	s.mu.Unlock()
	if ok {
		return res, nil
	}
	if page == nil {
		return tagResult{}, errors.New("page image missing")
	}
	box, err := tileBox(page.Bounds(), t.coords)
	if err != nil {
		return tagResult{}, err
	}
	sub, ok := page.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if !ok {
		return tagResult{}, errors.New("page image can't be cropped")
	}
	tag, err := s.Tags.Read(ctx, sub.SubImage(box), title)
	switch {
	case errors.Is(err, pricetag.ErrNoTitle):
		res = tagResult{}
	case err != nil:
		return tagResult{}, err
	default:
		res = tagResult{tag: tag, ok: true}
	}
	s.mu.Lock()
	if s.cache == nil {
		s.cache = map[string]tagResult{}
	}
	s.cache[t.key] = res
	s.mu.Unlock()
	return res, nil
}

// tileBox converts Red Pepper's percentage coordinates to pixels.
func tileBox(page image.Rectangle, coords string) (image.Rectangle, error) {
	f := strings.Split(coords, ",")
	if len(f) != 5 {
		return image.Rectangle{}, fmt.Errorf("bad coords %q", coords)
	}
	var v [5]float64
	for i, s := range f {
		n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil {
			return image.Rectangle{}, fmt.Errorf("bad coords %q", coords)
		}
		v[i] = n
	}
	l, t, w, h := v[0], v[1], v[3], v[4]
	x := func(p float64) int { return page.Min.X + int(p/100*float64(page.Dx())) }
	y := func(p float64) int { return page.Min.Y + int(p/100*float64(page.Dy())) }
	r := image.Rect(x(l), y(t), x(l+w), y(t+h)).Intersect(page)
	if r.Empty() {
		return r, fmt.Errorf("coords %q fall outside the page", coords)
	}
	return r, nil
}

func applyTag(d *Deal, tag pricetag.Tag) {
	d.Price = tag.Price
	d.Unit = tag.Unit
	d.SaleStory = tag.Story
	if tag.Regular > tag.Price && tag.Price > 0 {
		d.RegularPrice = tag.Regular
	}
	if tag.Save > 0 {
		d.Discount = tag.Save
		if d.RegularPrice == 0 && d.Price > 0 {
			d.RegularPrice = cents(d.Price + tag.Save)
		}
	}
}

func (s *ShopRiteCircular) do(ctx context.Context, path string) (*http.Response, error) {
	u := path
	if strings.HasPrefix(path, "/") {
		u = cmp.Or(s.BaseURL, DefaultRedPepperURL) + path
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: unexpected status %s", u, resp.Status)
	}
	return resp, nil
}

func (s *ShopRiteCircular) getJSON(ctx context.Context, path string, v any, opts ...json.Options) error {
	resp, err := s.do(ctx, path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := json.UnmarshalRead(resp.Body, v, opts...); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func (s *ShopRiteCircular) getImage(ctx context.Context, url string) (image.Image, error) {
	resp, err := s.do(ctx, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	img, _, err := image.Decode(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("decode page %s: %w", url, err)
	}
	return img, nil
}
