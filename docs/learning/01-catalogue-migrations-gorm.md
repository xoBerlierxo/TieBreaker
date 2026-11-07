# 01 — Catalogue: migrations, GORM, seeding, JWT

Milestone **M1**. This note covers how the car catalogue is stored, loaded and served.

---

## 1. Plain-SQL migrations with golang-migrate (not GORM AutoMigrate)

[migrations/](../../migrations/), [internal/store/migrate.go](../../internal/store/migrate.go)

- Each change to the schema is a numbered pair of files: `0001_create_variants.up.sql` and `.down.sql`. golang-migrate records the applied version in a `schema_migrations` table, so `up` applies only what's missing.
- **Why not AutoMigrate** (SPEC §3)? AutoMigrate guesses DDL from Go structs. It can't express `CHECK` constraints, trigram indexes or partial indexes well, it never drops anything, and you can't review its SQL before it runs in production. Plain SQL is explicit and reviewable.
- `//go:embed *.sql` puts the files *inside the binary* (`migrations.FS` is an `embed.FS`). `iofs.New(migrations.FS, ".")` turns that into a migration source, so the container needs no files on disk.
- golang-migrate takes a Postgres **advisory lock** while migrating, so two containers starting at once can't both apply `0001`.
- `context.AfterFunc(ctx, f)` (Go 1.21+) runs `f` when `ctx` is cancelled. We use it to ask the migrator to stop gracefully after the current migration, never half-way through one.

## 2. CHECK constraints: the database as the last line of defence

```sql
CONSTRAINT variants_ev_battery CHECK (fuel_type <> 'ev' OR battery_kwh IS NOT NULL)
```

Read that as: "either it isn't an EV, or it has a battery". `A OR B` is the SQL way to write the implication "if not A then B". The same rules are repeated in Go (`Variant.Validate`) to produce friendly errors, but the constraint still protects the data from any other writer, such as a manual `psql` session or a future bug.

## 3. GORM models: pointers for "optional", explicit tags

[internal/catalog/variant.go](../../internal/catalog/variant.go)

```go
SafetyRatingStars *int `gorm:"column:safety_rating_stars" json:"safety_rating_stars"`
```

- A `*int` can be `nil`, meaning NULL or unknown, which is different from `0` stars. SPEC §7 requires "not rated", never zero, so the type system enforces it.
- Explicit `column:` tags mean nobody has to guess GORM's naming rules (would `BatteryKWh` become `battery_k_wh`?).
- `default:gen_random_uuid()` on the ID makes GORM leave the column out of the INSERT and read the generated value back with `RETURNING`.
- `Omit(clause.Associations)` stops GORM from "helpfully" inserting the `Sources` slice itself. We insert the sources explicitly, in the same transaction.
- `Updates` with a struct skips zero values by default. `Select("*")` forces every column to be written, which is what a PUT (full replace) means.

## 4. Custom types: `sql.Scanner` and `driver.Valuer`

[internal/store/textarray.go](../../internal/store/textarray.go), [internal/catalog/date.go](../../internal/catalog/date.go)

`database/sql` moves values between Go and the driver using two small interfaces:

```go
type Scanner interface { Scan(src any) error }            // DB → Go
type Valuer  interface { Value() (driver.Value, error) }  // Go → DB
```

- `TextArray` implements both for Postgres `text[]` by delegating to pgx's array codec (`pgtype.Map.SQLScanner`), so no hand-written parser is needed.
- `Date` implements them for `date` columns, plus `json.Marshaler`/`Unmarshaler`, so seed files can say `"2026-11-07"`.
- Note the receivers: `Scan` has a **pointer** receiver (`*Date`) because it modifies the value; `Value` has a value receiver.

## 5. Strict JSON decoding

```go
dec := json.NewDecoder(r)
dec.DisallowUnknownFields()
```

By default `encoding/json` silently ignores fields it doesn't recognise, so a seed file with `"sigam"` instead of `"sigma"` would load with sigma = 0. `DisallowUnknownFields` turns the typo into an error. We use it for seed files, assumption values and API request bodies (`bindJSON`). `dec.More()` after decoding rejects trailing garbage.

## 6. Idempotent seeding in one transaction

[internal/catalog/seed.go](../../internal/catalog/seed.go)

- **Upsert:** look up by natural key (`slug`) and insert or replace. For assumptions, use `INSERT … ON CONFLICT (key, model_version) DO UPDATE` (GORM `clause.OnConflict`). Running the loader twice gives the same rows.
- Everything runs inside one `store.WithTx`, so if the third file is broken, nothing from the first two is half-written.

## 7. Typed configuration of the model: `AssumptionSet`

[internal/catalog/assumptions.go](../../internal/catalog/assumptions.go)

- `specsFor(state)` is a list of `{key, decode func}` pairs. This is a **table of behaviour**: adding an assumption means adding one entry, and the list doubles as `RequiredAssumptionKeys`.
- The decode functions are **closures** that capture a loop variable (`fuel := f`). Since Go 1.22 each loop iteration has its own variable, but the explicit copy keeps the intent obvious.
- All errors are collected and `errors.Join`ed, so you see every missing key at once.

## 8. JWT validation, the safe way

[internal/http/auth.go](../../internal/http/auth.go)

```go
jwt.NewParser(
    jwt.WithValidMethods([]string{"HS256"}),  // reject "alg": "none" and RS/HS confusion
    jwt.WithIssuer("tiebreaker"),
    jwt.WithAudience("tiebreaker-admin"),
    jwt.WithExpirationRequired(),
)
```

- A JWT carries its algorithm in its header. Without `WithValidMethods`, an attacker could send `alg: none` (no signature) or trick an RSA-configured server into HMAC verification using the public key as the secret.
- **401 vs 403:** 401 means "we don't know who you are" (no token, bad token). 403 means "we know who you are, but you're not allowed" (valid token without the admin role).
- We also reject tokens whose `exp − iat` is longer than 1 hour, even when the signature is valid, which limits the damage from a leaked token.

## 9. Interfaces at the consumer, again

`httpapi.CatalogService` is declared in the HTTP package with only the five methods the handlers call. `*catalog.Service` satisfies it implicitly; the tests use `fakeCatalog`. The handlers never import GORM.

---

## Questions you should be able to answer

1. **Why is the safety rating a `*int` and not an `int`? Trace what happens to a car with no rating, from the seed JSON (`null`) through GORM to the API's `warnings` array.**
   *Hint:* JSON `null` → nil pointer → SQL NULL → nil again on read → `VariantWarnings` adds `safety_not_rated`.

2. **Running `go run ./scripts/seed` twice leaves exactly the same rows. Explain the two mechanisms that make variants and assumptions idempotent, and what `WithTx` adds.**
   *Hint:* lookup by slug, then insert or replace with sources replaced; `ON CONFLICT … DO UPDATE` on the unique `(key, model_version)`; all-or-nothing.

3. **An attacker sends a JWT with header `{"alg":"none"}` and `role: "admin"`. Which exact line rejects it, and what status code do they get? What would happen without that line?**
   *Hint:* `WithValidMethods` → parse error → 401 UNAUTHORIZED.
