# encoding/json/v2

All JSON in the app uses `encoding/json/v2` and `encoding/json/jsontext`,
which ship with Go 1.27 and need no `GOEXPERIMENT`. The project's Go
guidelines say to use v2 for new JSON code. The rest of the code moved over
too, so there is one JSON package.

## What v2 helped with

Tested against the real responses saved from Flipp, Red Pepper and Costco:

- **Embedded objects.** Costco's coupon sets are JSON objects inside non-JSON
  text. `jsontext.NewDecoder(r).ReadValue()` reads exactly one value and
  returns its bytes, so the parser can skip past it. (v1's `Decoder.Decode`
  could also do this.)
- **Per-call time parsing.** Red Pepper's times are Eastern with no zone, both
  `2026-10-22T00:01` and `2026-10-21T00:00:00`. v1 rejects both. v2's
  `json.WithUnmarshalers(json.UnmarshalFunc(…))` parses them for one call
  with plain `time.Time` fields, and no wrapper type (`rpTimes` in
  `internal/deals/shoprite.go`).
- **Strict input.** v2 rejects invalid UTF-8 and duplicate object keys by
  default. All 69 saved responses passed, so this costs nothing and would
  surface corrupted input.

What didn't help:

- **Quoted numbers.** `json.StringifyNumbers(true)` reads Costco's
  `"value":["39.99"]` into `[]float64`, which v1 can't do. But Costco also
  sends `"1,199.99"`, which neither version parses, so the values are read as
  strings and parsed by hand. The `,string` tag option doesn't apply to slice
  elements in v2.
- **Speed.** Mixed and irrelevant for a weekly refresh: 6.8 ms against v1's
  9.4 ms on a 5.3 MB file, and 7.6 ms against 3.5 ms on a 1.3 MB one.

## Behavior changes from the migration

| Area | v1 | v2 | Effect here |
|---|---|---|---|
| Field names | matched case-insensitively | exact match | Deal files (`-deals-file`) must use the lowercase keys, e.g. `regular_price` |
| Map key order | sorted | unsorted unless `json.Deterministic(true)` | The snapshot file and `/api/plan` pass it, so output stays stable |
| nil slices | `null` | `[]` | `/api/plan` shows `"deals": []` rather than `null` |
| `omitempty` on numbers | omits 0 | never omits numbers | Numeric fields use `omitzero`, e.g. `regular_price,omitzero` |
| Invalid UTF-8, duplicate keys | accepted | rejected | Bad deal files fail to load with an error |
| Indented output | `MarshalIndent` | `jsontext.WithIndent` option | `store.Put` |

Snapshots written by the v1 build still load: the field names didn't change,
and v2 ignores unknown fields by default.
