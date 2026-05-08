// Package provider defines the Provider interface and the global model registry
// used by all image-generation adapters.
//
// Adding a new upstream provider requires only:
//  1. Implementing the [Provider] interface.
//  2. Registering it with [Register] in the provider's init() or the main setup.
//  3. Adding a Fake implementation in provider/<name>/fake for CI and unit tests.
//
// No other package needs to change when a new provider is added.
package provider
