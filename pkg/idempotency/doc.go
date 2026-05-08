// Package idempotency implements deduplication for generation jobs.
//
// When a client supplies an Idempotency-Key header, the gateway checks whether
// a successful response was already recorded for that key within the dedup
// window (default 5 minutes). If so, the cached response is returned without
// calling the upstream again, preventing duplicate charges.
//
// The in-memory Store is suitable for single-node deployments and tests.
// Replace with a Redis-backed store for multi-node production deployments.
package idempotency
