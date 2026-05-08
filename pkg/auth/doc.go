// Package auth validates platform-issued API keys and enforces the
// "server-side unified key holding" model (Mode A from the design document).
//
// Callers never see upstream provider keys. The platform issues its own
// short-lived or long-lived keys (stored as bcrypt/argon2 hashes in the DB);
// this package handles the validation and scope-checking middleware.
package auth
