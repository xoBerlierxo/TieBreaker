package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/xoBerlierxo/TieBreaker/internal/store"
)

// ModelFile is the format of data/seed/<model>.json: one file per car model
// (SPEC §7), holding one or more variants with their sources.
type ModelFile struct {
	Variants []Variant `json:"variants"`
}

// AssumptionFile is the format of data/seed/assumptions/*.json.
type AssumptionFile struct {
	ModelVersion string           `json:"model_version"`
	Assumptions  []AssumptionSeed `json:"assumptions"`
}

// AssumptionSeed is one assumption in a seed file.
type AssumptionSeed struct {
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
	SourceURL string          `json:"source_url"`
	CheckedAt *Date           `json:"checked_at"`
	Verified  bool            `json:"verified"`
	Note      string          `json:"note,omitempty"`
}

// SeedResult counts what LoadDir wrote.
type SeedResult struct {
	Variants    int
	Sources     int
	Assumptions int
}

// ReadSeedDir parses and validates every seed file without touching the
// database. Model files are the *.json files directly in dir; assumption files
// are in dir/assumptions. Unknown JSON fields are errors, so a typo is caught.
func ReadSeedDir(dir string) ([]Variant, []AssumptionFile, error) {
	modelPaths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, nil, fmt.Errorf("listing model files: %w", err)
	}
	assumptionPaths, err := filepath.Glob(filepath.Join(dir, "assumptions", "*.json"))
	if err != nil {
		return nil, nil, fmt.Errorf("listing assumption files: %w", err)
	}
	slices.Sort(modelPaths)
	slices.Sort(assumptionPaths)

	var variants []Variant
	slugs := map[string]string{}
	for _, path := range modelPaths {
		var mf ModelFile
		if err := decodeFileStrict(path, &mf); err != nil {
			return nil, nil, err
		}
		for _, v := range mf.Variants {
			if err := checkSeedVariant(v); err != nil {
				return nil, nil, fmt.Errorf("%s: variant %q: %w", path, v.Slug, err)
			}
			if prev, dup := slugs[v.Slug]; dup {
				return nil, nil, fmt.Errorf("%s: slug %q already defined in %s", path, v.Slug, prev)
			}
			slugs[v.Slug] = path
			variants = append(variants, v)
		}
	}

	var files []AssumptionFile
	keys := map[string]string{}
	for _, path := range assumptionPaths {
		var af AssumptionFile
		if err := decodeFileStrict(path, &af); err != nil {
			return nil, nil, err
		}
		if af.ModelVersion == "" {
			return nil, nil, fmt.Errorf("%s: model_version is required", path)
		}
		for _, a := range af.Assumptions {
			id := af.ModelVersion + "/" + a.Key
			if prev, dup := keys[id]; dup {
				return nil, nil, fmt.Errorf("%s: assumption %q already defined in %s", path, a.Key, prev)
			}
			keys[id] = path
			if a.Verified && (IsPlaceholderURL(a.SourceURL) || a.CheckedAt == nil) {
				return nil, nil, fmt.Errorf("%s: assumption %q is marked verified but has no real source and checked_at", path, a.Key)
			}
			if !json.Valid(a.Value) || len(a.Value) == 0 {
				return nil, nil, fmt.Errorf("%s: assumption %q has no valid value", path, a.Key)
			}
		}
		files = append(files, af)
	}
	return variants, files, nil
}

// checkSeedVariant validates a seed variant, including SPEC §5.1's rule that a
// verified variant must have a real source for every field.
func checkSeedVariant(v Variant) error {
	if err := v.Validate(); err != nil {
		return err
	}
	for _, s := range v.Sources {
		if s.FieldName == "" || s.SourceURL == "" || s.CheckedAt.IsZero() {
			return errors.New("every source needs field_name, source_url and checked_at")
		}
	}
	if v.Verified {
		return CanVerify(v, v.Sources)
	}
	return nil
}

// LoadDir reads the seed directory and upserts everything in one transaction:
// variants by slug (sources replaced), assumptions by (key, model_version).
// Running it twice leaves the same data, so it is safe to re-run.
func LoadDir(ctx context.Context, db *gorm.DB, dir string) (SeedResult, error) {
	variants, files, err := ReadSeedDir(dir)
	if err != nil {
		return SeedResult{}, err
	}

	var res SeedResult
	err = store.WithTx(ctx, db, func(tx *gorm.DB) error {
		for i := range variants {
			v := variants[i]
			var existing Variant
			err := tx.Select("id", "created_at").Where("slug = ?", v.Slug).First(&existing).Error
			switch {
			case errors.Is(err, gorm.ErrRecordNotFound):
				v.ID = ""
				if err := insertVariant(tx, &v); err != nil {
					return fmt.Errorf("seeding %s: %w", v.Slug, err)
				}
			case err != nil:
				return fmt.Errorf("looking up %s: %w", v.Slug, err)
			default:
				v.ID, v.CreatedAt = existing.ID, existing.CreatedAt
				if err := replaceVariant(tx, &v); err != nil {
					return fmt.Errorf("seeding %s: %w", v.Slug, err)
				}
			}
			res.Variants++
			res.Sources += len(v.Sources)
		}

		for _, f := range files {
			for _, a := range f.Assumptions {
				row := Assumption{
					Key: a.Key, ValueJSON: a.Value, SourceURL: a.SourceURL,
					CheckedAt: a.CheckedAt, ModelVersion: f.ModelVersion, Verified: a.Verified,
				}
				err := tx.Clauses(clause.OnConflict{
					Columns:   []clause.Column{{Name: "key"}, {Name: "model_version"}},
					DoUpdates: clause.Assignments(map[string]any{"value_json": row.ValueJSON, "source_url": row.SourceURL, "checked_at": row.CheckedAt, "verified": row.Verified, "updated_at": gorm.Expr("now()")}),
				}).Create(&row).Error
				if err != nil {
					return fmt.Errorf("seeding assumption %s: %w", a.Key, err)
				}
				res.Assumptions++
			}
		}
		return nil
	})
	if err != nil {
		return SeedResult{}, err
	}
	return res, nil
}

func decodeFileStrict(path string, dst any) error {
	b, err := os.ReadFile(path) // #nosec G304 -- path comes from a glob over the seed directory
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}
	if strings.TrimSpace(string(b)) == "" {
		return fmt.Errorf("%s is empty", path)
	}
	return nil
}

// AssumptionRows flattens parsed assumption files into table rows, so callers
// (tests, tools) can build an AssumptionSet without a database.
func AssumptionRows(files []AssumptionFile) []Assumption {
	var rows []Assumption
	for _, f := range files {
		for _, a := range f.Assumptions {
			rows = append(rows, Assumption{Key: a.Key, ValueJSON: a.Value, SourceURL: a.SourceURL,
				CheckedAt: a.CheckedAt, ModelVersion: f.ModelVersion, Verified: a.Verified})
		}
	}
	return rows
}
