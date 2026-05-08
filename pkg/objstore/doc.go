// Package objstore provides a unified interface for object storage operations
// across S3, Aliyun OSS, and MinIO.
//
// All provider adapters that produce image data upload via this interface.
// The concrete implementation (S3-compatible) lives in internal/storage.
// Tests use the in-memory Fake.
package objstore
