# ADR-003 — Token/cost optimisation, artwork storage, and community features

**Date**: 2026-05-08  
**Status**: Accepted  
**Author**: @pyq

## 1. Work visibility

Works (generated images saved by users) use a three-value visibility enum.
New works default to `private`.

| Value     | Who can see | Appears in community feed |
|---|---|---|
| `private`   | Owner + admin only | No |
| `unlisted`  | Anyone with the exact link | No |
| `public`    | All authenticated users | Yes (after moderation) |

Implementation: stored as a `TEXT` column with a CHECK constraint in the
`works` table. Changing visibility requires ownership or admin scope.

## 2. Community paid SKUs

SKU IDs are stable strings stored in the `entitlements` table.

| SKU ID                  | Description |
|---|---|
| `community:browse`      | Unlimited community feed browsing |
| `community:hd_download` | Download original high-res assets |
| `community:hide_own`    | Exclude own public works from community recommendations |
| `community:bulk_export` | Batch download up to 100 own works |

SKUs are checked at the route level by the `auth` middleware reading the
caller's entitlements. Callers without `community:browse` see only their
own works via `/jobs`.

## 3. Thumbnail and CDN strategy

### Upload pipeline

```
Provider returns image (URL or base64)
          │
    Download to memory (< 10 MiB safety cap)
          │
    Upload original → OSS/S3 bucket: works/{user_id}/{job_id}/original.{ext}
          │
    Async worker: generate 2 thumbnail variants
          ├─ 480px → works/{user_id}/{job_id}/thumb_480.webp
          └─ 1200px → works/{user_id}/{job_id}/thumb_1200.webp
```

### Access control via signed URLs

- Community feed: serve `thumb_480.webp` via CDN with a 24-hour public cache
  (low sensitivity, reduces signing overhead).
- Work detail page: serve `thumb_1200.webp` via a 1-hour pre-signed URL.
- Original file: serve via a 15-minute pre-signed URL, gated by
  `community:hd_download` entitlement.

### CDN configuration

- Separate CDN distribution (or path prefix) for community thumbnails vs.
  private originals.
- Private originals: CDN caching disabled; always proxy to storage for auth.
- Community thumbnails: CDN TTL = 24h; invalidated on visibility change.

## 4. Log sanitisation

### Fields that must never appear in logs

| Field | Safe alternative |
|---|---|
| `api_key` (raw) | `key_id` (opaque DB ID) |
| `Authorization` header | omit entirely |
| Upstream API keys | omit; log provider name only |
| Full prompt text | `prompt_hash` (first 8 chars of SHA256) |
| IP address | Only in access log at infra level; not in app logs |

### Implementation

`pkg/logger.Redact(s string)` is called for any field that might carry a
credential. The `withLogging` HTTP middleware must redact the `Authorization`
header before logging request metadata.

Structured log fields used consistently across all components:

```
trace_id, api_key_id, user_id, job_id, provider, model,
status, latency_ms, upstream_status, prompt_hash
```

## 5. Upstream retry boundaries

| Condition | Retry? | Max attempts | Back-off |
|---|---|---|---|
| Network timeout (before response) | Yes | 2 | jitter exp |
| HTTP 429 with `Retry-After` | Yes (honor header) | 3 | per header |
| HTTP 502/503/504 | Yes | 2 | jitter exp |
| HTTP 400/401/403/422 | No — client error | 1 | — |
| HTTP 500 from upstream | No — upstream bug | 1 | — |
| Context cancelled by user | No | 1 | — |

After all retries are exhausted the job transitions to `failed` and the
pre-deduction is reversed (see ADR-002).

The `pkg/httpclient` package implements these rules. Provider adapters must
not implement their own retry logic; they delegate to `httpclient.Client.Do`.

## Consequences

- Works default to `private` prevents accidental public exposure.
- The three-tier thumbnail strategy balances cost (serve cheap thumbnails to
  the feed) and quality (serve originals only when the user explicitly requests
  them).
- Log field policy prevents credentials from leaking into monitoring systems
  (Datadog, ELK, etc.) via log shipping.
- The retry boundary table is machine-readable (implemented in
  `pkg/httpclient.shouldRetry`); adapter authors do not need to memorise it.
