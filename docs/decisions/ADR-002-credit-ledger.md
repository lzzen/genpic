# ADR-002 — Credit ledger: pre-deduct / actual-deduct / reversal

**Date**: 2026-05-08  
**Status**: Accepted  
**Author**: @pyq

## Context

The platform must account for the cost of every image generation job. The
challenge is that:

1. The upstream cost is not known until the job completes (or the upstream
   returns it).
2. Jobs can fail mid-flight — the platform should not charge users for failures.
3. A user's credit balance must never go negative in steady state.

## Decision

A **three-step ledger pattern** is used:

```
   Client request
        │
   [1] PreDeduct(estimate)  ─── blocks if balance < estimate
        │
   Dispatch to upstream
        │
   ┌────┴──────────────┐
   │                   │
  success            failure
   │                   │
[2] Finalise(actual)  [3] Reverse(job_id, reason)
   │                   │
 Job succeeded       Job failed, credit restored
```

### Ledger table schema (PostgreSQL)

```sql
CREATE TABLE credit_ledger (
    id          TEXT        PRIMARY KEY DEFAULT gen_ulid(),
    user_id     TEXT        NOT NULL,
    job_id      TEXT,                               -- NULL for top-up entries
    kind        TEXT        NOT NULL,               -- pre_deduct | actual_deduct | reversal | top_up
    credits     BIGINT      NOT NULL,               -- positive = credit; negative = debit
    model_id    TEXT,
    note        TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_ledger_user_id ON credit_ledger (user_id, created_at DESC);
CREATE INDEX idx_ledger_job_id  ON credit_ledger (job_id) WHERE job_id IS NOT NULL;
```

### Balance query

The effective balance is the SUM of all credit rows for a user:

```sql
SELECT COALESCE(SUM(credits), 0) AS balance
FROM   credit_ledger
WHERE  user_id = $1;
```

Pre-deductions are negative; reversals offset them with a positive row;
actual-deductions replace the estimate by:
1. Inserting a reversal for the pre-deduction amount.
2. Inserting an actual-deduct row for the real amount.

This keeps the ledger append-only and auditable.

### Isolation

Both the balance check and the pre-deduction INSERT must run in the same
serialisable transaction to prevent double-spending under concurrent requests:

```sql
BEGIN ISOLATION LEVEL SERIALIZABLE;
  SELECT COALESCE(SUM(credits), 0) INTO v_balance FROM credit_ledger WHERE user_id = $1 FOR UPDATE;
  IF v_balance < $2 THEN ROLLBACK; RAISE 'insufficient_balance'; END IF;
  INSERT INTO credit_ledger (user_id, job_id, kind, credits, model_id, note)
    VALUES ($1, $3, 'pre_deduct', -$2, $4, 'pre-deduction for job');
COMMIT;
```

### Cost estimation

Each model has a price entry in the pricing table (not yet implemented; planned
for M4). For M0/M1, a conservative fixed estimate per image is used:

| Provider | Unit          | Estimate (credits) |
|---|---|---|
| openai/gpt-image-2    | per image     | 100 |
| gemini/*              | per 1K tokens | 1   |
| wan/wan2.7-image      | per image     | 50  |
| wan/wan2.7-image-pro  | per image     | 100 |

### Retry safety

Pre-deduction uses the job's idempotency key; re-submitting the same job ID
does not create a second pre-deduction row (the existing one is returned).

### Failure rollback

The job worker always issues a Reverse on:
- upstream timeout
- upstream non-retryable error (4xx)
- worker crash on startup (at-least-once recovery: scan for `pre_deduct` rows
  with no matching `actual_deduct` or `reversal` older than 10 minutes)

## State machine alignment

```
Job status    →    Ledger action
──────────────────────────────────────
queued         →   PreDeduct
running        →   (no new entry)
succeeded      →   Finalise (reversal + actual_deduct)
failed         →   Reverse
```

## Consequences

- The ledger is always consistent even when the worker process crashes
  mid-flight, because the reversal is idempotent and the recovery scanner
  issues it on restart.
- The three-step pattern allows the platform to over-estimate (safer) and
  refund the difference on success, rather than under-estimating and charging
  retroactively.
- The append-only schema makes the ledger auditable and easy to replay for
  billing disputes.
