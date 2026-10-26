package deals

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

// File loads deals from a JSON array on disk. It backs the Costco monthly
// coupon book (entered by hand or from another scraper) and offline demos.
type File struct {
	Path string
}

func (f *File) Name() string { return "file:" + f.Path }

func (f *File) Fetch(_ context.Context, _ []string) ([]Deal, error) {
	b, err := os.ReadFile(f.Path)
	if err != nil {
		return nil, err
	}
	var ds []Deal
	if err := json.Unmarshal(b, &ds); err != nil {
		return nil, fmt.Errorf("parse %s: %w", f.Path, err)
	}
	for i := range ds {
		if ds[i].Source == "" {
			ds[i].Source = f.Name()
		}
		if ds[i].ID == "" {
			ds[i].ID = fmt.Sprintf("%s-%d", f.Path, i)
		}
	}
	return ds, nil
}
