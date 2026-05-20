package objstore

import (
	"context"
	"fmt"
	"io"
	"time"
)

// PutInput describes an object to upload.
type PutInput struct {
	// Bucket is the target bucket. If empty, the default bucket is used.
	Bucket string
	// Key is the object key (path within the bucket).
	Key string
	// Body is the object data. The caller is responsible for closing it.
	Body io.Reader
	// ContentType is the MIME type, e.g. "image/png".
	ContentType string
	// ContentLength is the byte length of Body; 0 means unknown.
	ContentLength int64
}

// PutResult is returned after a successful upload.
type PutResult struct {
	// ETag is the server-assigned entity tag (typically a hex MD5/SHA256).
	ETag string
	// Key echoes the input key for caller convenience.
	Key string
}

// SignedURLInput describes a pre-signed URL request.
type SignedURLInput struct {
	Bucket    string
	Key       string
	ExpiresIn time.Duration
}

// Store is the object storage interface.
type Store interface {
	// Put uploads an object and returns metadata.
	Put(ctx context.Context, in PutInput) (PutResult, error)
	// Get downloads an object by logical key.
	Get(ctx context.Context, key string) ([]byte, string, error)
	// SignedURL generates a time-limited HTTPS URL for the given object.
	SignedURL(ctx context.Context, in SignedURLInput) (string, error)
	// Delete removes an object. Returns nil if the object was not found.
	Delete(ctx context.Context, bucket, key string) error
}

// Fake is an in-memory Store for tests. It stores uploaded bytes in a map
// and generates deterministic "signed" URLs.
type Fake struct {
	Objects map[string][]byte // key → content
}

func NewFake() *Fake { return &Fake{Objects: map[string][]byte{}} }

func (f *Fake) Get(_ context.Context, key string) ([]byte, string, error) {
	if b, ok := f.Objects[key]; ok {
		return b, "application/octet-stream", nil
	}
	return nil, "", fmt.Errorf("objstore fake: not found")
}

func (f *Fake) Put(_ context.Context, in PutInput) (PutResult, error) {
	data, err := io.ReadAll(in.Body)
	if err != nil {
		return PutResult{}, err
	}
	f.Objects[in.Key] = data
	return PutResult{ETag: "fake-etag", Key: in.Key}, nil
}

func (f *Fake) SignedURL(_ context.Context, in SignedURLInput) (string, error) {
	return "https://fake-store.test/" + in.Key + "?signed=1", nil
}

func (f *Fake) Delete(_ context.Context, _, key string) error {
	delete(f.Objects, key)
	return nil
}
