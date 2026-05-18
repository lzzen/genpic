package storage

import (
	"context"
	"strings"
	"testing"
	"time"

	"genpic/pkg/objstore"
)

func TestNewS3Compatible_MissingBucket(t *testing.T) {
	_, err := NewS3Compatible(context.Background(), S3Config{
		AccessKey: "a",
		SecretKey: "b",
	})
	if err == nil || !strings.Contains(err.Error(), "bucket") {
		t.Fatalf("want bucket error, got %v", err)
	}
}

func TestNewS3Compatible_MissingAccessKey(t *testing.T) {
	_, err := NewS3Compatible(context.Background(), S3Config{
		Bucket:    "b",
		SecretKey: "s",
	})
	if err == nil || !strings.Contains(err.Error(), "access_key") {
		t.Fatalf("want access_key error, got %v", err)
	}
}

func TestNewS3Compatible_MissingSecretKey(t *testing.T) {
	_, err := NewS3Compatible(context.Background(), S3Config{
		Bucket:    "b",
		AccessKey: "a",
	})
	if err == nil || !strings.Contains(err.Error(), "secret_key") {
		t.Fatalf("want secret_key error, got %v", err)
	}
}

func TestNewS3Compatible_OK_Minimal(t *testing.T) {
	s, err := NewS3Compatible(context.Background(), S3Config{
		Bucket:    "my-bucket",
		AccessKey: "ACCESSKEY",
		SecretKey: "SECRETKEY",
	})
	if err != nil {
		t.Fatal(err)
	}
	if s == nil || s.bucket != "my-bucket" {
		t.Fatalf("store: %#v", s)
	}
}

func TestS3Store_PublicURL(t *testing.T) {
	s, err := NewS3Compatible(context.Background(), S3Config{
		Region:        "ap-guangzhou",
		Bucket:        "pic",
		AccessKey:     "a",
		SecretKey:     "s",
		PublicBaseURL: "https://cdn.example.com",
		KeyPrefix:     "genpic",
	})
	if err != nil {
		t.Fatal(err)
	}
	u := s.PublicURL("users/u1/jobs/abc/0.png")
	want := "https://cdn.example.com/genpic/users/u1/jobs/abc/0.png"
	if u != want {
		t.Fatalf("PublicURL: got %q want %q", u, want)
	}
}

func TestS3Store_SignedURL_LocalPresign(t *testing.T) {
	s, err := NewS3Compatible(context.Background(), S3Config{
		Region:    "us-east-1",
		Bucket:    "b",
		AccessKey: "AKIAIOSFODNN7EXAMPLE",
		SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	})
	if err != nil {
		t.Fatal(err)
	}
	url, err := s.SignedURL(context.Background(), objstore.SignedURLInput{
		Key:       "path/to/obj.png",
		ExpiresIn: 5 * time.Minute,
	})
	if err != nil || url == "" {
		t.Fatalf("SignedURL: err=%v url=%q", err, url)
	}
	if !strings.HasPrefix(url, "http") {
		t.Fatalf("SignedURL not http(s): %q", url)
	}
}
