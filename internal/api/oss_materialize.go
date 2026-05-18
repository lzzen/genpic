package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"genpic/internal/jobstore"
	"genpic/internal/userstorage"
	pkgerrors "genpic/pkg/errors"
	"genpic/pkg/objstore"
	"genpic/pkg/provider"
)

func randomHex(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func deleteOSSObjects(ctx context.Context, st objstore.Store, logicalKeys []string) {
	for _, k := range logicalKeys {
		_ = st.Delete(ctx, "", strings.TrimSpace(k))
	}
}

// uploadReferenceImagesToOSS uploads decoded reference blobs for a logged-in user.
func uploadReferenceImagesToOSS(ctx context.Context, userID string, refs []provider.ReferenceImage) ([]jobstore.JobRefAsset, error) {
	st := getObjectStore()
	db := getQuotaDB()
	if st == nil || userID == "" {
		return nil, nil
	}
	if len(refs) == 0 {
		return nil, nil
	}
	var sum int64
	decoded := make([][]byte, 0, len(refs))
	mimes := make([]string, 0, len(refs))
	for i, r := range refs {
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(r.B64))
		if err != nil {
			return nil, pkgerrors.BadRequest("invalid_reference", fmt.Sprintf("reference_images[%d]: invalid base64", i))
		}
		mt := strings.TrimSpace(r.MIMEType)
		if mt == "" {
			mt = "image/png"
		}
		decoded = append(decoded, raw)
		mimes = append(mimes, mt)
		sum += int64(len(raw))
	}
	if db != nil {
		ok, _, _, err := userstorage.CheckQuota(ctx, db, userID, sum)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, pkgerrors.BadRequest("storage_quota_exceeded", "reference images exceed your storage quota")
		}
	}

	var keys []string
	var ledger []userstorage.LedgerRow
	var assets []jobstore.JobRefAsset

	for i := range decoded {
		suffix, err := randomHex(8)
		if err != nil {
			deleteOSSObjects(ctx, st, keys)
			return nil, err
		}
		ext := extForMIME(mimes[i])
		logical := fmt.Sprintf("users/%s/refs/%s.%s", userID, suffix, ext)
		keys = append(keys, logical)
		_, err = st.Put(ctx, objstore.PutInput{
			Key:             logical,
			Body:            bytes.NewReader(decoded[i]),
			ContentType:     mimes[i],
			ContentLength:   int64(len(decoded[i])),
		})
		if err != nil {
			deleteOSSObjects(ctx, st, keys)
			return nil, pkgerrors.Wrap(http.StatusBadGateway, pkgerrors.TypeInternal, "oss_put_reference", "could not upload reference image", err)
		}
		pub, err := resolveObjectHTTPSURL(ctx, logical)
		if err != nil {
			deleteOSSObjects(ctx, st, keys)
			return nil, err
		}
		assets = append(assets, jobstore.JobRefAsset{URL: pub, MIMEType: mimes[i]})
		ledger = append(ledger, userstorage.LedgerRow{
			ObjectKey: logical,
			ByteSize:  int64(len(decoded[i])),
			Kind:      "reference",
		})
	}
	if db != nil && len(ledger) > 0 {
		if err := userstorage.AppendLedgerAfterUpload(ctx, db, userID, ledger); err != nil {
			if err == userstorage.ErrQuotaExceeded {
				deleteOSSObjects(ctx, st, keys)
				return nil, pkgerrors.BadRequest("storage_quota_exceeded", "reference images exceed your storage quota")
			}
			deleteOSSObjects(ctx, st, keys)
			return nil, err
		}
	}
	return assets, nil
}

func readLocalArtifactBytes(jobID, artifactURL string) ([]byte, string, error) {
	u, err := url.Parse(strings.TrimSpace(artifactURL))
	if err != nil || u.Path == "" {
		return nil, "", fmt.Errorf("not a local artifact url")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 4 || parts[0] != "api" || parts[1] != "artifacts" {
		return nil, "", fmt.Errorf("unexpected path")
	}
	if parts[2] != jobID {
		return nil, "", fmt.Errorf("job id mismatch")
	}
	name := parts[len(parts)-1]
	root := artifactsDir()
	if root == "" {
		return nil, "", fmt.Errorf("artifacts disabled")
	}
	p := filepath.Join(root, jobID, filepath.Base(name))
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, "", err
	}
	return b, mimeFromArtifactExt(filepath.Ext(name)), nil
}

func imageBytesForJobOutput(ctx context.Context, jobID string, d *imageData, fetchHosts []string, maxFetch int64, fetchTimeout time.Duration) ([]byte, string, error) {
	if strings.TrimSpace(d.B64JSON) != "" {
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(d.B64JSON))
		if err != nil {
			return nil, "", err
		}
		mt := strings.TrimSpace(d.MimeType)
		if mt == "" {
			mt = "image/png"
		}
		return raw, mt, nil
	}
	u := strings.TrimSpace(d.URL)
	if u == "" {
		return nil, "", fmt.Errorf("empty image slot")
	}
	if strings.HasPrefix(u, "/api/artifacts/") {
		return readLocalArtifactBytes(jobID, u)
	}
	if len(fetchHosts) == 0 {
		return nil, "", fmt.Errorf("remote url fetch disabled")
	}
	b, ct, err := FetchRemoteHTTPSImage(ctx, u, fetchHosts, maxFetch, fetchTimeout)
	if err != nil {
		return nil, "", err
	}
	mt := strings.TrimSpace(d.MimeType)
	if mt == "" {
		mt = strings.TrimSpace(ct)
	}
	if mt == "" {
		mt = "image/png"
	}
	return b, mt, nil
}

// materializeJobOutputsOSS uploads generation results for a logged-in user and replaces URLs in out.
func materializeJobOutputsOSS(ctx context.Context, jobID, userID string, out map[string]any) error {
	st := getObjectStore()
	db := getQuotaDB()
	if st == nil || userID == "" || out == nil {
		return fmt.Errorf("oss materialize: missing store or user")
	}
	raw, ok := out["data"].([]imageData)
	if !ok || len(raw) == 0 {
		return nil
	}
	fetchHosts, maxFetch, fetchTimeout := ossFetchPolicy()

	var keys []string
	var ledger []userstorage.LedgerRow
	next := make([]imageData, len(raw))
	copy(next, raw)

	for i := range next {
		slot := &next[i]
		b, mime, err := imageBytesForJobOutput(ctx, jobID, slot, fetchHosts, maxFetch, fetchTimeout)
		if err != nil {
			// Keep upstream URL when we cannot fetch / decode (e.g. remote fetch disabled).
			if strings.TrimSpace(slot.URL) != "" && strings.TrimSpace(slot.B64JSON) == "" {
				continue
			}
			deleteOSSObjects(ctx, st, keys)
			return pkgerrors.Wrap(http.StatusBadGateway, pkgerrors.TypeInternal, "oss_image_bytes", fmt.Sprintf("image %d", i), err)
		}
		ext := extForMIME(mime)
		pKey := fmt.Sprintf("users/%s/jobs/%s/%d.%s", userID, jobID, i, ext)
		keys = append(keys, pKey)
		if _, err := st.Put(ctx, objstore.PutInput{
			Key:             pKey,
			Body:            bytes.NewReader(b),
			ContentType:     mime,
			ContentLength:   int64(len(b)),
		}); err != nil {
			deleteOSSObjects(ctx, st, keys)
			return pkgerrors.Wrap(http.StatusBadGateway, pkgerrors.TypeInternal, "oss_put_output", fmt.Sprintf("image %d", i), err)
		}
		pub, err := resolveObjectHTTPSURL(ctx, pKey)
		if err != nil {
			deleteOSSObjects(ctx, st, keys)
			return err
		}
		slot.URL = pub
		slot.B64JSON = ""
		slot.MimeType = mime
		ledger = append(ledger, userstorage.LedgerRow{
			ObjectKey: pKey,
			ByteSize:  int64(len(b)),
			Kind:      "output",
			JobID:     jobID,
		})

		thumbBytes, err := encodeJPEGThumbToBytes(b, mime)
		if err == nil && len(thumbBytes) > 0 {
			tKey := fmt.Sprintf("users/%s/jobs/%s/%d_thumb.jpg", userID, jobID, i)
			keys = append(keys, tKey)
			if _, err := st.Put(ctx, objstore.PutInput{
				Key:             tKey,
				Body:            bytes.NewReader(thumbBytes),
				ContentType:     "image/jpeg",
				ContentLength:   int64(len(thumbBytes)),
			}); err == nil {
				tu, err := resolveObjectHTTPSURL(ctx, tKey)
				if err == nil {
					slot.ThumbURL = tu
				}
				ledger = append(ledger, userstorage.LedgerRow{
					ObjectKey: tKey,
					ByteSize:  int64(len(thumbBytes)),
					Kind:      "output_thumb",
					JobID:     jobID,
				})
			}
		}
	}

	if db != nil && len(ledger) > 0 {
		if err := userstorage.AppendLedgerAfterUpload(ctx, db, userID, ledger); err != nil {
			if err == userstorage.ErrQuotaExceeded {
				deleteOSSObjects(ctx, st, keys)
				return pkgerrors.BadRequest("storage_quota_exceeded", "generated images exceed your storage quota")
			}
			deleteOSSObjects(ctx, st, keys)
			return err
		}
	}

	out["data"] = next
	return nil
}

// uploadTemplateReferenceMapsToOSS replaces large base64 template references with OSS URLs for logged-in users.
func uploadTemplateReferenceMapsToOSS(ctx context.Context, userID string, refs []map[string]any) ([]map[string]any, error) {
	st := getObjectStore()
	db := getQuotaDB()
	if st == nil || userID == "" || len(refs) == 0 {
		return refs, nil
	}
	type dec struct {
		raw []byte
		mt  string
	}
	var decoded []dec
	var sum int64
	for i, m := range refs {
		b64, _ := m["b64_json"].(string)
		b64 = strings.TrimSpace(strings.ReplaceAll(b64, " ", ""))
		if b64 == "" {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, pkgerrors.BadRequest("invalid_reference_images", fmt.Sprintf("reference %d: invalid base64", i))
		}
		mt, _ := m["mime_type"].(string)
		mt = strings.TrimSpace(mt)
		if mt == "" {
			mt = "image/png"
		}
		decoded = append(decoded, dec{raw: raw, mt: mt})
		sum += int64(len(raw))
	}
	if len(decoded) == 0 {
		return refs, nil
	}
	if db != nil {
		ok, _, _, err := userstorage.CheckQuota(ctx, db, userID, sum)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, pkgerrors.BadRequest("storage_quota_exceeded", "template reference images exceed your storage quota")
		}
	}

	tplRun, err := randomHex(8)
	if err != nil {
		return nil, err
	}
	var keys []string
	var ledger []userstorage.LedgerRow
	out := make([]map[string]any, 0, len(refs))
	di := 0
	for _, m := range refs {
		b64, _ := m["b64_json"].(string)
		if strings.TrimSpace(strings.ReplaceAll(b64, " ", "")) == "" {
			out = append(out, m)
			continue
		}
		if di >= len(decoded) {
			break
		}
		d := decoded[di]
		di++
		ext := extForMIME(d.mt)
		logical := fmt.Sprintf("users/%s/templates/%s/%d.%s", userID, tplRun, len(keys), ext)
		keys = append(keys, logical)
		if _, err := st.Put(ctx, objstore.PutInput{
			Key:             logical,
			Body:            bytes.NewReader(d.raw),
			ContentType:     d.mt,
			ContentLength:   int64(len(d.raw)),
		}); err != nil {
			deleteOSSObjects(ctx, st, keys)
			return nil, pkgerrors.Wrap(http.StatusBadGateway, pkgerrors.TypeInternal, "oss_put_template_ref", "could not upload template reference", err)
		}
		pub, err := resolveObjectHTTPSURL(ctx, logical)
		if err != nil {
			deleteOSSObjects(ctx, st, keys)
			return nil, err
		}
		ledger = append(ledger, userstorage.LedgerRow{
			ObjectKey: logical,
			ByteSize:  int64(len(d.raw)),
			Kind:      "template",
		})
		out = append(out, map[string]any{
			"mime_type": d.mt,
			"url":       pub,
		})
	}
	if db != nil && len(ledger) > 0 {
		if err := userstorage.AppendLedgerAfterUpload(ctx, db, userID, ledger); err != nil {
			if err == userstorage.ErrQuotaExceeded {
				deleteOSSObjects(ctx, st, keys)
				return nil, pkgerrors.BadRequest("storage_quota_exceeded", "template reference images exceed your storage quota")
			}
			deleteOSSObjects(ctx, st, keys)
			return nil, err
		}
	}
	return out, nil
}
