// Package ratelimit provides a sliding-window rate limiter keyed by arbitrary
// string dimensions (API key, user ID, IP address, or global).
//
// The in-memory implementation is suitable for single-instance deployments.
// For multi-instance deployments, swap in the Redis-backed implementation
// (planned for M4) behind the same [Limiter] interface.
package ratelimit
