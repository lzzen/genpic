// Package objstore provides a unified interface for object storage operations
// across S3-compatible endpoints (Tencent COS, Aliyun OSS, MinIO, AWS S3).
//
// The default implementation for cmd/genpic lives in internal/storage (S3 API).
// Tests use the in-memory Fake.
package objstore
