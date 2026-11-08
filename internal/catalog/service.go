package catalog

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/xoBerlierxo/TieBreaker/internal/apperr"
	"github.com/xoBerlierxo/TieBreaker/internal/store"
)

// Search limits (SPEC §12: GET /v1/variants?q=&fuel=&limit=).
const (
	DefaultSearchLimit = 20
	MaxSearchLimit     = 50
	maxQueryLength     = 100
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Service is the catalogue's business logic and data access. Handlers call it;
// it never knows about HTTP.
type Service struct {
	db *gorm.DB
}

// NewService creates a catalogue service.
func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

// SearchParams are the catalogue search inputs. Zero values mean "not set".
type SearchParams struct {
	Query string
	Fuel  FuelType
	Limit int
}

// Search finds variants whose "make model variant" contains the query
// (case-insensitive), optionally filtered by fuel type, ordered by name.
func (s *Service) Search(ctx context.Context, p SearchParams) ([]Variant, error) {
	q := strings.TrimSpace(p.Query)
	if len(q) > maxQueryLength {
		return nil, apperr.Validation("q", fmt.Sprintf("q must be at most %d characters", maxQueryLength))
	}
	if p.Fuel != "" && !p.Fuel.Valid() {
		return nil, apperr.Validation("fuel", "fuel must be one of petrol, diesel, cng, ev, strong_hybrid")
	}
	limit := p.Limit
	switch {
	case limit == 0:
		limit = DefaultSearchLimit
	case limit < 0 || limit > MaxSearchLimit:
		return nil, apperr.Validation("limit", fmt.Sprintf("limit must be between 1 and %d", MaxSearchLimit))
	}

	tx := s.db.WithContext(ctx).Model(&Variant{})
	if q != "" {
		// The expression matches the trigram index in migration 0001 exactly,
		// so Postgres can use it. LIKE wildcards in the user's query are escaped.
		tx = tx.Where("(make || ' ' || model || ' ' || variant_name) ILIKE ?", "%"+escapeLike(q)+"%")
	}
	if p.Fuel != "" {
		tx = tx.Where("fuel_type = ?", p.Fuel)
	}

	var out []Variant
	if err := tx.Order("make, model, variant_name").Limit(limit).Find(&out).Error; err != nil {
		return nil, fmt.Errorf("searching variants: %w", err)
	}
	return out, nil
}

// Get returns one variant with its sources. An unknown or malformed ID is
// "not found" (a malformed UUID would otherwise be a Postgres syntax error).
func (s *Service) Get(ctx context.Context, id string) (Variant, error) {
	if !uuidPattern.MatchString(id) {
		return Variant{}, apperr.New(apperr.ErrNotFound, "variant not found")
	}
	var v Variant
	err := s.db.WithContext(ctx).
		Preload("Sources", func(db *gorm.DB) *gorm.DB { return db.Order("field_name, id") }).
		Preload("OnRoadPrices", func(db *gorm.DB) *gorm.DB { return db.Order("state") }).
		First(&v, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Variant{}, apperr.New(apperr.ErrNotFound, "variant not found")
	}
	if err != nil {
		return Variant{}, fmt.Errorf("loading variant %s: %w", id, err)
	}
	return v, nil
}

// GetMany returns the variants with the given IDs, keyed by ID. Missing IDs are
// simply absent from the map; callers decide whether that's an error.
func (s *Service) GetMany(ctx context.Context, ids []string) (map[string]Variant, error) {
	valid := make([]string, 0, len(ids))
	for _, id := range ids {
		if uuidPattern.MatchString(id) {
			valid = append(valid, id)
		}
	}
	out := make(map[string]Variant, len(valid))
	if len(valid) == 0 {
		return out, nil
	}
	var rows []Variant
	// On-road prices are needed by the session's pricing; sources are not.
	if err := s.db.WithContext(ctx).Preload("OnRoadPrices").Where("id IN ?", valid).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("loading variants: %w", err)
	}
	for _, v := range rows {
		out[v.ID] = v
	}
	return out, nil
}

// Create validates and inserts a variant together with its sources, in one
// transaction. A variant may only be created as verified if CanVerify passes.
func (s *Service) Create(ctx context.Context, v Variant) (Variant, error) {
	if err := s.checkWritable(v); err != nil {
		return Variant{}, err
	}
	v.ID = ""
	err := store.WithTx(ctx, s.db, func(tx *gorm.DB) error {
		return insertVariant(tx, &v)
	})
	if err != nil {
		return Variant{}, err
	}
	return s.Get(ctx, v.ID)
}

// Update replaces a variant's data and its sources.
func (s *Service) Update(ctx context.Context, id string, v Variant) (Variant, error) {
	if !uuidPattern.MatchString(id) {
		return Variant{}, apperr.New(apperr.ErrNotFound, "variant not found")
	}
	if err := s.checkWritable(v); err != nil {
		return Variant{}, err
	}
	err := store.WithTx(ctx, s.db, func(tx *gorm.DB) error {
		var existing Variant
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&existing, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return apperr.New(apperr.ErrNotFound, "variant not found")
			}
			return fmt.Errorf("locking variant: %w", err)
		}
		v.ID, v.CreatedAt = id, existing.CreatedAt
		return replaceVariant(tx, &v)
	})
	if err != nil {
		return Variant{}, err
	}
	return s.Get(ctx, id)
}

// Delete removes a variant. A variant used in a session's shortlist can't be
// deleted (the foreign key is ON DELETE RESTRICT).
func (s *Service) Delete(ctx context.Context, id string) error {
	if !uuidPattern.MatchString(id) {
		return apperr.New(apperr.ErrNotFound, "variant not found")
	}
	res := s.db.WithContext(ctx).Delete(&Variant{}, "id = ?", id)
	if errors.Is(res.Error, gorm.ErrForeignKeyViolated) {
		return apperr.New(apperr.ErrConflict, "variant is used in a shortlist and cannot be deleted")
	}
	if res.Error != nil {
		return fmt.Errorf("deleting variant: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return apperr.New(apperr.ErrNotFound, "variant not found")
	}
	return nil
}

func (s *Service) checkWritable(v Variant) error {
	if err := v.Validate(); err != nil {
		return err
	}
	for _, src := range v.Sources {
		if strings.TrimSpace(src.FieldName) == "" || strings.TrimSpace(src.SourceURL) == "" {
			return apperr.Validation("sources", "every source needs field_name and source_url")
		}
		if src.CheckedAt.IsZero() {
			return apperr.Validation("sources", "every source needs checked_at")
		}
	}
	if v.Verified {
		return CanVerify(v, v.Sources)
	}
	return nil
}

// insertVariant inserts v and its sources using tx. v.ID is filled in by the
// database default.
func insertVariant(tx *gorm.DB, v *Variant) error {
	sources, prices := v.Sources, v.OnRoadPrices
	v.Sources, v.OnRoadPrices = nil, nil
	if err := tx.Omit(clause.Associations).Create(v).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return apperr.Validation("slug", "a variant with this slug already exists")
		}
		return fmt.Errorf("inserting variant: %w", err)
	}
	if err := insertSources(tx, v.ID, sources); err != nil {
		return err
	}
	if err := replaceOnRoadPrices(tx, v.ID, prices); err != nil {
		return err
	}
	v.Sources, v.OnRoadPrices = sources, prices
	return nil
}

// replaceVariant overwrites every column of an existing variant and replaces its sources.
func replaceVariant(tx *gorm.DB, v *Variant) error {
	sources, prices := v.Sources, v.OnRoadPrices
	v.Sources, v.OnRoadPrices = nil, nil
	if err := tx.Omit(clause.Associations).Select("*").Omit("id", "created_at").
		Where("id = ?", v.ID).Updates(v).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return apperr.Validation("slug", "a variant with this slug already exists")
		}
		return fmt.Errorf("updating variant: %w", err)
	}
	if err := tx.Where("variant_id = ?", v.ID).Delete(&VariantSource{}).Error; err != nil {
		return fmt.Errorf("deleting old sources: %w", err)
	}
	if err := insertSources(tx, v.ID, sources); err != nil {
		return err
	}
	if err := replaceOnRoadPrices(tx, v.ID, prices); err != nil {
		return err
	}
	v.Sources, v.OnRoadPrices = sources, prices
	return nil
}

func insertSources(tx *gorm.DB, variantID string, sources []VariantSource) error {
	if len(sources) == 0 {
		return nil
	}
	for i := range sources {
		sources[i].ID = 0
		sources[i].VariantID = variantID
	}
	if err := tx.Create(&sources).Error; err != nil {
		return fmt.Errorf("inserting sources: %w", err)
	}
	return nil
}

// escapeLike escapes the LIKE wildcards % and _ (and the escape character
// itself), so a user typing "100%" searches for that literal text.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// AllNames returns every make, model and display name in the catalogue. The
// LLM fact validator uses it to reject prose that names a car not in the report.
func (s *Service) AllNames(ctx context.Context) ([]string, error) {
	var rows []Variant
	if err := s.db.WithContext(ctx).Select("make", "model", "variant_name").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("loading catalogue names: %w", err)
	}
	seen := map[string]bool{}
	var out []string
	for _, v := range rows {
		for _, n := range []string{v.Make, v.Model, v.Make + " " + v.Model, v.DisplayName()} {
			if !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	return out, nil
}
