// Package recipes holds the recipe catalog the planner chooses from.
package recipes

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Ingredient is one recipe component. Match lists lowercase substrings that
// identify a deal for this ingredient; Exclude rules out false positives
// (e.g. "chicken broth" when we want chicken meat).
type Ingredient struct {
	Name    string   `json:"name"`
	Match   []string `json:"match"`
	Exclude []string `json:"exclude,omitempty"`
	Main    bool     `json:"main,omitempty"` // the protein/centerpiece; weighted higher
}

// Recipe is a dinner the planner can schedule.
type Recipe struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Protein     string       `json:"protein"` // used to keep the week varied
	Minutes     int          `json:"minutes"`
	Servings    int          `json:"servings"`
	URL         string       `json:"url,omitempty"`
	Ingredients []Ingredient `json:"ingredients"`
}

// Matches reports whether a deal name satisfies this ingredient.
func (in Ingredient) Matches(dealName string) bool {
	n := strings.ToLower(dealName)
	for _, ex := range in.Exclude {
		if strings.Contains(n, ex) {
			return false
		}
	}
	for _, m := range in.Match {
		if strings.Contains(n, m) {
			return true
		}
	}
	return false
}

//go:embed recipes.json
var builtin []byte

// Builtin returns the embedded starter catalog.
func Builtin() ([]Recipe, error) { return parse(builtin, "builtin") }

// Load reads a catalog from path, or the builtin catalog when path is empty.
func Load(path string) ([]Recipe, error) {
	if path == "" {
		return Builtin()
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parse(b, path)
}

func parse(b []byte, name string) ([]Recipe, error) {
	var rs []Recipe
	if err := json.Unmarshal(b, &rs); err != nil {
		return nil, fmt.Errorf("parse recipes %s: %w", name, err)
	}
	return rs, nil
}

// SearchTerms returns the distinct first match term of every ingredient,
// which is what we ask deal sources to search for.
func SearchTerms(rs []Recipe) []string {
	set := map[string]bool{}
	for _, r := range rs {
		for _, in := range r.Ingredients {
			if len(in.Match) > 0 {
				set[in.Match[0]] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}
