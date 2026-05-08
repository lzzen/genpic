package billing

import (
	"context"
	"time"
)

// EntryKind classifies a ledger row.
type EntryKind string

const (
	// KindPreDeduct is a tentative reservation before the upstream is called.
	KindPreDeduct EntryKind = "pre_deduct"
	// KindActualDeduct replaces the pre-deduction with the real cost on success.
	KindActualDeduct EntryKind = "actual_deduct"
	// KindReversal cancels a pre-deduction when the job fails.
	KindReversal EntryKind = "reversal"
	// KindTopUp credits the account (purchase, admin grant, etc.).
	KindTopUp EntryKind = "top_up"
)

// Entry is a single row in the credit ledger.
type Entry struct {
	ID        string
	UserID    string
	JobID     string // empty for top-ups
	Kind      EntryKind
	Credits   int64 // positive = credit; negative = debit
	Note      string
	CreatedAt time.Time
}

// CostEstimate is the predicted credit cost before calling the upstream.
// Adapters compute it from the model pricing table so the pre-deduction
// can block jobs when the user has insufficient balance.
type CostEstimate struct {
	// Credits is the estimated cost in platform credits.
	Credits int64
	// ModelID is included for audit purposes.
	ModelID string
}

// Ledger is the interface for credit accounting operations.
// The concrete implementation queries the database in a transaction to
// maintain consistency. A no-op implementation is provided for tests.
type Ledger interface {
	// Balance returns the current credit balance for the user.
	// Returns ErrInsufficientBalance when the balance is zero or negative.
	Balance(ctx context.Context, userID string) (int64, error)

	// PreDeduct reserves estimated credits for a job about to be dispatched.
	// Returns ErrInsufficientBalance when the balance would go negative.
	PreDeduct(ctx context.Context, userID, jobID string, est CostEstimate) error

	// Finalise replaces the pre-deduction with the actual cost.
	// actualCredits should be the real cost reported by the upstream or
	// computed from usage metrics; pass 0 to accept the estimate as final.
	Finalise(ctx context.Context, userID, jobID string, actualCredits int64) error

	// Reverse cancels the pre-deduction for a failed job.
	Reverse(ctx context.Context, userID, jobID string, reason string) error
}

// ErrInsufficientBalance is returned when a user has insufficient credits.
var ErrInsufficientBalance = billingError("insufficient_balance")

type billingError string

func (e billingError) Error() string { return string(e) }

// NoopLedger is a Ledger that accepts all operations without checking balances.
// Use in tests and in MVP Lite (no billing at the Lite stage).
type NoopLedger struct{}

func (NoopLedger) Balance(_ context.Context, _ string) (int64, error)             { return 999999, nil }
func (NoopLedger) PreDeduct(_ context.Context, _, _ string, _ CostEstimate) error { return nil }
func (NoopLedger) Finalise(_ context.Context, _, _ string, _ int64) error         { return nil }
func (NoopLedger) Reverse(_ context.Context, _, _ string, _ string) error         { return nil }
