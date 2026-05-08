// Package billing defines the credit accounting types and the Ledger interface.
//
// The billing flow for each generation job is:
//
//  1. Pre-deduct: reserve estimated credits before dispatching to the upstream.
//  2. Finalise: on success, replace the estimate with the actual cost.
//  3. Reverse: on failure, issue a credit reversal so the user is not charged.
//
// This three-step pattern ensures that failures are never silently absorbed and
// that the ledger is always consistent, even when a worker crashes mid-flight.
//
// The credit unit is an internal "credit" that the platform maps to
// real-money cost. The exact exchange rate is managed by billing configuration
// and is intentionally not hard-coded here.
package billing
