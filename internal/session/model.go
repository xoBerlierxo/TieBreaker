// Package session holds a buyer's decision session (SPEC §5.2, §6): the
// shortlist, the hard constraints, the interview answers, and the state
// machine that moves the session from shortlisting to a finished report. Every
// change goes through optimistic locking on Session.Version.
package session

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"time"
)

// NewID returns 128 random bits from crypto/rand, base64url-encoded without
// padding: 22 URL-safe characters (SPEC §14: ≥128 bits, unguessable). It is
// used for session IDs, share IDs and simulation-run IDs.
func NewID() string {
	var b [16]byte
	// crypto/rand.Read never fails on supported platforms (Go 1.24+).
	_, _ = rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// Session is one row of the sessions table.
type Session struct {
	ID              string          `gorm:"column:id;primaryKey"`
	Version         int             `gorm:"column:version"`
	State           State           `gorm:"column:state"`
	UserState       string          `gorm:"column:user_state"`
	ConstraintsJSON json.RawMessage `gorm:"column:constraints_json;type:jsonb"`
	CreatedAt       time.Time       `gorm:"column:created_at"`
	UpdatedAt       time.Time       `gorm:"column:updated_at"`
}

// TableName tells GORM the table name explicitly.
func (Session) TableName() string { return "sessions" }

// Condition is new or used.
type Condition string

const (
	ConditionNew  Condition = "new"
	ConditionUsed Condition = "used"
)

// Item is one car on the shortlist (SPEC §5.1 ShortlistItem).
type Item struct {
	ID                     string          `gorm:"column:id;type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	SessionID              string          `gorm:"column:session_id" json:"-"`
	VariantID              string          `gorm:"column:variant_id;type:uuid" json:"variant_id"`
	Position               int             `gorm:"column:position" json:"position"`
	Condition              Condition       `gorm:"column:condition" json:"condition"`
	AskingPriceINR         *int64          `gorm:"column:asking_price_inr" json:"asking_price_inr,omitempty"`
	Year                   *int            `gorm:"column:year" json:"year,omitempty"`
	OdometerKm             *int            `gorm:"column:odometer_km" json:"odometer_km,omitempty"`
	OwnersCount            *int            `gorm:"column:owners_count" json:"owners_count,omitempty"`
	ConditionGrade         *string         `gorm:"column:condition_grade" json:"condition_grade,omitempty"`
	OnRoadPriceOverrideINR *int64          `gorm:"column:on_road_price_override_inr" json:"on_road_price_override_inr,omitempty"`
	Eliminated             bool            `gorm:"column:eliminated" json:"eliminated"`
	EliminationJSON        json.RawMessage `gorm:"column:elimination_json;type:jsonb" json:"-"`
	CreatedAt              time.Time       `gorm:"column:created_at" json:"-"`
}

// TableName tells GORM the table name explicitly.
func (Item) TableName() string { return "shortlist_items" }

// Answer is one stored interview answer (SPEC §5.3).
type Answer struct {
	ID         int64           `gorm:"column:id;primaryKey"`
	SessionID  string          `gorm:"column:session_id"`
	QuestionID string          `gorm:"column:question_id"`
	AnswerJSON json.RawMessage `gorm:"column:answer_json;type:jsonb"`
	CreatedAt  time.Time       `gorm:"column:created_at"`
}

// TableName tells GORM the table name explicitly.
func (Answer) TableName() string { return "answers" }

// IdempotencyKey is one row of idempotency_keys (SPEC §5.5).
type IdempotencyKey struct {
	SessionID      string    `gorm:"column:session_id;primaryKey"`
	Key            string    `gorm:"column:key;primaryKey"`
	RequestHash    string    `gorm:"column:request_hash"`
	ResponseStatus *int      `gorm:"column:response_status"`
	ResponseJSON   *string   `gorm:"column:response_json;type:text"` // exact bytes, so replays are identical
	CreatedAt      time.Time `gorm:"column:created_at"`
}

// TableName tells GORM the table name explicitly.
func (IdempotencyKey) TableName() string { return "idempotency_keys" }
