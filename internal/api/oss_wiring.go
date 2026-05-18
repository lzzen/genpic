package api

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"time"

	"genpic/pkg/mvpconfig"
	"genpic/pkg/objstore"
)

var (
	objectStoreMu sync.RWMutex
	objectStore   objstore.Store
	// objectURL resolves a logical object key to an HTTPS URL for clients.
	objectURL func(context.Context, string) (string, error)

	quotaMu  sync.RWMutex
	quotaDB  *sql.DB
	ossCfg   mvpconfig.ObjectStorageConfig
)

// SetObjectStore wires S3-compatible storage; nil disables OSS uploads.
func SetObjectStore(s objstore.Store) {
	objectStoreMu.Lock()
	objectStore = s
	objectStoreMu.Unlock()
}

func getObjectStore() objstore.Store {
	objectStoreMu.RLock()
	defer objectStoreMu.RUnlock()
	return objectStore
}

// SetObjectURLResolver sets how logical keys become HTTPS URLs (CDN base or presigned URLs).
func SetObjectURLResolver(fn func(context.Context, string) (string, error)) {
	objectStoreMu.Lock()
	objectURL = fn
	objectStoreMu.Unlock()
}

// SetQuotaDB sets the MySQL pool used for storage quota (same DB as auth / jobs).
func SetQuotaDB(db *sql.DB) {
	quotaMu.Lock()
	quotaDB = db
	quotaMu.Unlock()
}

func getQuotaDB() *sql.DB {
	quotaMu.RLock()
	defer quotaMu.RUnlock()
	return quotaDB
}

// SetOSSMaterializeConfig copies object-storage tuning used by the API layer.
func SetOSSMaterializeConfig(c mvpconfig.ObjectStorageConfig) {
	quotaMu.Lock()
	ossCfg = c
	quotaMu.Unlock()
}

func ossArtifactMode() string {
	quotaMu.RLock()
	defer quotaMu.RUnlock()
	m := strings.ToLower(strings.TrimSpace(ossCfg.ArtifactMode))
	if m == "" {
		return "oss"
	}
	return m
}

func ossFetchPolicy() (hosts []string, maxBytes int64, timeout time.Duration) {
	quotaMu.RLock()
	defer quotaMu.RUnlock()
	h := ossCfg.URLFetchHosts
	if len(h) == 0 {
		return nil, ossCfg.MaxFetchBytes, ossCfg.FetchTimeout
	}
	cp := append([]string(nil), h...)
	return cp, ossCfg.MaxFetchBytes, ossCfg.FetchTimeout
}

func resolveObjectHTTPSURL(ctx context.Context, logicalKey string) (string, error) {
	objectStoreMu.RLock()
	fn := objectURL
	objectStoreMu.RUnlock()
	if fn == nil {
		return "", errNoObjectURLResolver
	}
	return fn(ctx, logicalKey)
}

var errNoObjectURLResolver = errNoObjectURL{}

type errNoObjectURL struct{}

func (errNoObjectURL) Error() string { return "api: object URL resolver not configured" }
