// Package storage provides S3-compatible object storage (Tencent COS, Aliyun OSS, MinIO).
package storage

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"genpic/pkg/objstore"
)

// S3Config configures an S3-API-compatible endpoint (COS, OSS, MinIO, AWS).
type S3Config struct {
	Region         string
	Endpoint       string // optional custom base URL, e.g. https://cos.ap-guangzhou.myqcloud.com
	Bucket         string
	AccessKey      string
	SecretKey      string
	UsePathStyle   bool
	PublicBaseURL  string // optional CDN / public origin for object URLs (no trailing slash)
	KeyPrefix      string // optional prefix for logical namespacing, e.g. genpic
}

// S3Store implements objstore.Store against an S3-compatible HTTP API.
type S3Store struct {
	client        *s3.Client
	presign       *s3.PresignClient
	bucket        string
	publicBaseURL string
	keyPrefix     string
}

// NewS3Compatible builds an S3 API client. endpoint may be empty for AWS default.
func NewS3Compatible(_ context.Context, cfg S3Config) (*S3Store, error) {
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, fmt.Errorf("storage: bucket is required")
	}
	if strings.TrimSpace(cfg.AccessKey) == "" || strings.TrimSpace(cfg.SecretKey) == "" {
		return nil, fmt.Errorf("storage: access_key and secret_key are required")
	}
	region := strings.TrimSpace(cfg.Region)
	if region == "" {
		region = "us-east-1"
	}
	awscfg := aws.Config{
		Region:      region,
		Credentials: credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
	}
	client := s3.NewFromConfig(awscfg, func(o *s3.Options) {
		if ep := strings.TrimSpace(cfg.Endpoint); ep != "" {
			ep = strings.TrimRight(ep, "/")
			o.BaseEndpoint = aws.String(ep)
		}
		o.UsePathStyle = cfg.UsePathStyle
	})
	prefix := strings.Trim(cfg.KeyPrefix, "/")
	pub := strings.TrimRight(strings.TrimSpace(cfg.PublicBaseURL), "/")
	return &S3Store{
		client:        client,
		presign:       s3.NewPresignClient(client),
		bucket:        cfg.Bucket,
		publicBaseURL: pub,
		keyPrefix:     prefix,
	}, nil
}

func (s *S3Store) fullKey(key string) string {
	key = strings.TrimLeft(key, "/")
	if s.keyPrefix == "" {
		return key
	}
	return s.keyPrefix + "/" + key
}

// PublicURL returns an HTTPS URL for a stored object when PublicBaseURL is configured.
func (s *S3Store) PublicURL(objectKey string) string {
	if s.publicBaseURL == "" {
		return ""
	}
	return s.publicBaseURL + "/" + strings.TrimLeft(s.fullKey(objectKey), "/")
}

func (s *S3Store) Get(ctx context.Context, key string) ([]byte, string, error) {
	fk := s.fullKey(key)
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(fk),
	})
	if err != nil {
		return nil, "", err
	}
	defer out.Body.Close()
	b, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, "", err
	}
	ct := "application/octet-stream"
	if out.ContentType != nil && strings.TrimSpace(*out.ContentType) != "" {
		ct = strings.TrimSpace(*out.ContentType)
	}
	return b, ct, nil
}

func (s *S3Store) Put(ctx context.Context, in objstore.PutInput) (objstore.PutResult, error) {
	key := in.Key
	if strings.TrimSpace(in.Bucket) != "" {
		return objstore.PutResult{}, fmt.Errorf("storage: per-put bucket override is not supported")
	}
	fk := s.fullKey(key)
	bucket := s.bucket
	var body io.Reader = in.Body
	var cl *int64
	if in.ContentLength > 0 {
		n := in.ContentLength
		cl = &n
	}
	ct := strings.TrimSpace(in.ContentType)
	if ct == "" {
		ct = "application/octet-stream"
	}
	out, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(bucket),
		Key:           aws.String(fk),
		Body:          body,
		ContentType:   aws.String(ct),
		ContentLength: cl,
	})
	if err != nil {
		return objstore.PutResult{}, err
	}
	etag := ""
	if out.ETag != nil {
		etag = strings.Trim(*out.ETag, `"`)
	}
	return objstore.PutResult{ETag: etag, Key: key}, nil
}

func (s *S3Store) SignedURL(ctx context.Context, in objstore.SignedURLInput) (string, error) {
	bucket := s.bucket
	if strings.TrimSpace(in.Bucket) != "" {
		bucket = in.Bucket
	}
	fk := s.fullKey(in.Key)
	exp := in.ExpiresIn
	if exp <= 0 {
		exp = time.Hour
	}
	out, err := s.presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(fk),
	}, s3.WithPresignExpires(exp))
	if err != nil {
		return "", err
	}
	return out.URL, nil
}

func (s *S3Store) Delete(ctx context.Context, bucket, key string) error {
	b := s.bucket
	if strings.TrimSpace(bucket) != "" {
		b = bucket
	}
	fk := s.fullKey(key)
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(b),
		Key:    aws.String(fk),
	})
	return err
}
