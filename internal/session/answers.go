package session

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/xoBerlierxo/TieBreaker/internal/apperr"
	"github.com/xoBerlierxo/TieBreaker/internal/interview"
	"github.com/xoBerlierxo/TieBreaker/internal/store"
)

// AnswerRequest is the body of POST /v1/sessions/:id/answers.
type AnswerRequest struct {
	QuestionID string          `json:"question_id"`
	Answer     json.RawMessage `json:"answer"`
}

// AnswerResponse is stored with the idempotency key and returned to the client.
type AnswerResponse struct {
	QuestionID string `json:"question_id"`
	Accepted   bool   `json:"accepted"`
	State      State  `json:"state"`
	Version    int    `json:"version"`
}

// AnswerOutcome is what SubmitAnswer returns: the HTTP status and body to send,
// and whether they were replayed from an earlier identical request.
type AnswerOutcome struct {
	Status   int
	Body     json.RawMessage
	Replayed bool
	Version  int
}

// RequestHash fingerprints a request body independent of key order and
// whitespace: decode to generic JSON, re-encode (encoding/json sorts map
// keys), then SHA-256. Numbers are kept exactly (UseNumber).
func RequestHash(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var generic any
	if err := dec.Decode(&generic); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(generic)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// ValidateIdempotencyKey checks the Idempotency-Key header value.
func ValidateIdempotencyKey(key string) error {
	if strings.TrimSpace(key) == "" {
		return apperr.Validation("Idempotency-Key", "the Idempotency-Key header is required")
	}
	if len(key) > 128 {
		return apperr.Validation("Idempotency-Key", "the Idempotency-Key header must be at most 128 characters")
	}
	return nil
}

// SubmitAnswer stores one answer (SPEC §12: requires Idempotency-Key and If-Match).
//
// Everything happens in ONE transaction, in this order:
//
//  1. Claim the idempotency key: INSERT … ON CONFLICT DO NOTHING. If the key
//     already exists, an identical request gets the stored response (even if
//     the version has moved on since) and a different body gets 422. If another
//     request with the same key is still running, Postgres makes this INSERT
//     wait until that transaction ends.
//  2. Optimistic-locking version bump (stale If-Match → 409).
//  3. Validate the answer for the current state, store it, maybe change state.
//  4. Save the response on the key row.
//
// If any step fails, the whole transaction rolls back, including the key, so
// the client can fix the problem and retry with the same key.
func (s *Service) SubmitAnswer(ctx context.Context, id string, version int, key string, req AnswerRequest) (AnswerOutcome, error) {
	if err := ValidateIdempotencyKey(key); err != nil {
		return AnswerOutcome{}, err
	}
	if req.QuestionID == "" || len(req.Answer) == 0 {
		return AnswerOutcome{}, apperr.Validation("body", `expected {"question_id": "...", "answer": {...}}`)
	}
	hash, err := RequestHash(req)
	if err != nil {
		return AnswerOutcome{}, apperr.Validation("answer", "answer must be valid JSON")
	}

	var out AnswerOutcome
	err = store.WithTx(ctx, s.db, func(tx *gorm.DB) error {
		var exists int64
		if err := tx.Model(&Session{}).Where("id = ?", id).Count(&exists).Error; err != nil {
			return fmt.Errorf("checking session: %w", err)
		}
		if exists == 0 {
			return apperr.New(apperr.ErrNotFound, "session not found")
		}

		// 1. Claim the key.
		claim := tx.Clauses(clause.OnConflict{DoNothing: true}).
			Create(&IdempotencyKey{SessionID: id, Key: key, RequestHash: hash})
		if claim.Error != nil {
			return fmt.Errorf("claiming idempotency key: %w", claim.Error)
		}
		if claim.RowsAffected == 0 {
			var prior IdempotencyKey
			if err := tx.First(&prior, "session_id = ? AND key = ?", id, key).Error; err != nil {
				return fmt.Errorf("reading idempotency key: %w", err)
			}
			if prior.RequestHash != hash {
				return apperr.New(apperr.ErrIdempotencyMismatch, "this Idempotency-Key was already used with a different request body")
			}
			if prior.ResponseStatus == nil || prior.ResponseJSON == nil {
				return apperr.New(apperr.ErrConflict, "a request with this Idempotency-Key is still being processed")
			}
			out = AnswerOutcome{Status: *prior.ResponseStatus, Body: json.RawMessage(*prior.ResponseJSON), Replayed: true}
			return nil
		}

		// 2–3. Version bump and the answer itself.
		sess, err := bumpVersion(tx, id, version)
		if err != nil {
			return err
		}
		newState, err := s.applyAnswer(ctx, tx, sess, req)
		if err != nil {
			return err
		}

		// 4. Store the response for replays.
		body, err := json.Marshal(AnswerResponse{QuestionID: req.QuestionID, Accepted: true, State: newState, Version: sess.Version})
		if err != nil {
			return fmt.Errorf("encoding response: %w", err)
		}
		status := http.StatusCreated
		if err := tx.Model(&IdempotencyKey{}).Where("session_id = ? AND key = ?", id, key).
			Updates(map[string]any{"response_status": status, "response_json": string(body)}).Error; err != nil {
			return fmt.Errorf("saving idempotent response: %w", err)
		}
		out = AnswerOutcome{Status: status, Body: body, Version: sess.Version}
		return nil
	})
	if err != nil {
		return AnswerOutcome{}, err
	}
	return out, nil
}

// applyAnswer validates and stores the answer for the session's current state
// and returns the state afterwards.
func (s *Service) applyAnswer(ctx context.Context, tx *gorm.DB, sess Session, req AnswerRequest) (State, error) {
	snap, err := s.load(ctx, tx, sess.ID)
	if err != nil {
		return "", err
	}
	snap.sess = sess

	switch sess.State {
	case StateUsage:
		answered := map[string]bool{}
		for _, a := range snap.answers {
			answered[a.QuestionID] = true
		}
		next := interview.NextUsageQuestion(answered)
		if next == nil || next.ID != req.QuestionID {
			want := "none"
			if next != nil {
				want = next.ID
			}
			return "", apperr.New(apperr.ErrIllegalTransition,
				fmt.Sprintf("usage questions are answered in order; the next one is %s", want)).WithDetails("expected_question_id", want)
		}
		normalized, err := interview.ValidateUsageAnswer(req.QuestionID, req.Answer)
		if err != nil {
			return "", err
		}
		if err := insertAnswer(tx, sess.ID, req.QuestionID, normalized); err != nil {
			return "", err
		}
		answered[req.QuestionID] = true
		if interview.NextUsageQuestion(answered) == nil {
			if err := setState(tx, sess, StateInterview); err != nil {
				return "", err
			}
			return StateInterview, nil
		}
		return StateUsage, nil

	case StateInterview:
		if !strings.HasPrefix(req.QuestionID, "tradeoff:") {
			return "", apperr.New(apperr.ErrIllegalTransition, "the usage questions are finished; answer the trade-off questions")
		}
		st, err := snap.tradeoffState()
		if err != nil {
			return "", err
		}
		attrs := interview.Candidates(snap.survivingVariants())
		step, err := interview.ValidateTradeoffAnswer(attrs, st, s.cfg.Interview.Ladder, s.cfg.Interview.MaxQuestions, req.QuestionID, req.Answer)
		if err != nil {
			return "", err
		}
		raw, err := json.Marshal(step)
		if err != nil {
			return "", fmt.Errorf("encoding trade-off step: %w", err)
		}
		if err := insertAnswer(tx, sess.ID, req.QuestionID, raw); err != nil {
			return "", err
		}
		return StateInterview, nil

	default:
		return "", apperr.New(apperr.ErrIllegalTransition, fmt.Sprintf("answers are not accepted in state %s", sess.State))
	}
}

func insertAnswer(tx *gorm.DB, sessionID, questionID string, raw json.RawMessage) error {
	err := tx.Create(&Answer{SessionID: sessionID, QuestionID: questionID, AnswerJSON: raw}).Error
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return apperr.New(apperr.ErrConflict, "this question has already been answered")
	}
	if err != nil {
		return fmt.Errorf("storing answer: %w", err)
	}
	return nil
}
