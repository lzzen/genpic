// Package errors defines the unified error vocabulary for the Genpic platform.
//
// All API errors are serialised as OpenAI-compatible JSON bodies:
//
//	{"error":{"type":"invalid_request","code":"prompt_too_long","message":"..."}}
//
// Callers should construct errors with New or Wrap, not raw fmt.Errorf, so that
// the HTTP layer can extract type/code without type-switching on raw strings.
package errors
