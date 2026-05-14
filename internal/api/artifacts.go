package api

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	pkgerrors "genpic/pkg/errors"
)

var (
	artifactsMu          sync.RWMutex
	artifactsRoot        string // empty → materialization disabled
	validJobID           = regexp.MustCompile(`^[a-f0-9]{32}$`)
	validPrimaryArtifact = regexp.MustCompile(`^([0-9]+)\.(png|jpg|jpeg|webp|gif)$`)
	validThumbArtifact   = regexp.MustCompile(`^([0-9]+)_thumb\.jpg$`)
)

// SetArtifactsRoot sets the on-disk directory for saved generation images.
// Empty root disables writing files (responses keep upstream b64/url as-is).
func SetArtifactsRoot(dir string) {
	artifactsMu.Lock()
	defer artifactsMu.Unlock()
	artifactsRoot = dir
}

func artifactsDir() string {
	artifactsMu.RLock()
	defer artifactsMu.RUnlock()
	return artifactsRoot
}

// materializeJobImages writes each non-empty b64_json in out["data"] to disk and
// replaces it with a same-origin URL /api/artifacts/{jobID}/{i}.{ext}.
func materializeJobImages(jobID string, out map[string]any) error {
	root := artifactsDir()
	if root == "" || out == nil {
		return nil
	}
	if !validJobID.MatchString(jobID) {
		return fmt.Errorf("invalid job id")
	}
	raw, ok := out["data"].([]imageData)
	if !ok || len(raw) == 0 {
		return nil
	}

	jobDir := filepath.Join(root, jobID)
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		return fmt.Errorf("mkdir artifacts: %w", err)
	}

	next := make([]imageData, len(raw))
	copy(next, raw)

	for i := range next {
		if strings.TrimSpace(next[i].B64JSON) == "" {
			continue
		}
		b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(next[i].B64JSON))
		if err != nil {
			return fmt.Errorf("image %d: decode base64: %w", i, err)
		}
		ext := extForMIME(next[i].MimeType)
		fn := fmt.Sprintf("%d.%s", i, ext)
		path := filepath.Join(jobDir, fn)
		if err := os.WriteFile(path, b, 0o644); err != nil {
			return fmt.Errorf("image %d: write %s: %w", i, fn, err)
		}
		next[i].URL = "/api/artifacts/" + jobID + "/" + fn
		next[i].B64JSON = ""
		thumbFn := fmt.Sprintf("%d_thumb.jpg", i)
		thumbPath := filepath.Join(jobDir, thumbFn)
		if err := writeJPEGThumbFile(b, next[i].MimeType, thumbPath); err == nil {
			next[i].ThumbURL = "/api/artifacts/" + jobID + "/" + thumbFn
		}
	}

	out["data"] = next
	return nil
}

func mimeFromArtifactExt(ext string) string {
	switch strings.ToLower(strings.TrimPrefix(ext, ".")) {
	case "jpg", "jpeg":
		return "image/jpeg"
	case "webp":
		return "image/webp"
	case "gif":
		return "image/gif"
	default:
		return "image/png"
	}
}

// ensureArtifactThumb creates {index}_thumb.jpg from the primary artifact on disk
// when the thumbnail is missing (e.g. jobs created before previews existed).
func ensureArtifactThumb(jobDir string, thumbNameLower string) error {
	m := validThumbArtifact.FindStringSubmatch(thumbNameLower)
	if m == nil {
		return fmt.Errorf("invalid thumb filename")
	}
	index := m[1]
	thumbPath := filepath.Join(jobDir, thumbNameLower)
	if fi, err := os.Stat(thumbPath); err == nil && !fi.IsDir() {
		return nil
	}
	exts := []string{"png", "jpg", "jpeg", "webp", "gif"}
	for _, ext := range exts {
		p := filepath.Join(jobDir, index+"."+ext)
		fi, err := os.Stat(p)
		if err != nil || fi.IsDir() {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return writeJPEGThumbFile(b, mimeFromArtifactExt(ext), thumbPath)
	}
	return fmt.Errorf("primary artifact not found for index %s", index)
}

func extForMIME(mime string) string {
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "image/jpeg", "image/jpg":
		return "jpg"
	case "image/webp":
		return "webp"
	case "image/gif":
		return "gif"
	default:
		return "png"
	}
}

// HandleServeArtifact serves GET /api/artifacts/{job_id}/{name}.
func HandleServeArtifact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	root := artifactsDir()
	if root == "" {
		Error(w, pkgerrors.New(http.StatusNotFound, pkgerrors.TypeNotFound, "artifacts_disabled", "artifact storage is not configured"))
		return
	}

	jobID := strings.TrimSpace(r.PathValue("job_id"))
	name := strings.TrimSpace(r.PathValue("name"))
	nameLower := strings.ToLower(filepath.Base(name))
	if !validJobID.MatchString(jobID) {
		Error(w, pkgerrors.NotFound("artifact"))
		return
	}
	if !validPrimaryArtifact.MatchString(nameLower) && !validThumbArtifact.MatchString(nameLower) {
		Error(w, pkgerrors.NotFound("artifact"))
		return
	}

	rootAbs, err := filepath.Abs(root)
	if err != nil {
		Error(w, pkgerrors.New(http.StatusInternalServerError, pkgerrors.TypeInternal, "artifact_path", err.Error()))
		return
	}
	jobDir := filepath.Join(rootAbs, jobID)
	jobDirAbs, err := filepath.Abs(jobDir)
	if err != nil {
		Error(w, pkgerrors.New(http.StatusInternalServerError, pkgerrors.TypeInternal, "artifact_path", err.Error()))
		return
	}

	if validThumbArtifact.MatchString(nameLower) {
		if err := ensureArtifactThumb(jobDirAbs, nameLower); err != nil {
			Error(w, pkgerrors.NotFound("artifact"))
			return
		}
	}

	fullAbs, err := filepath.Abs(filepath.Join(jobDirAbs, nameLower))
	if err != nil {
		Error(w, pkgerrors.New(http.StatusInternalServerError, pkgerrors.TypeInternal, "artifact_path", err.Error()))
		return
	}
	rel, err := filepath.Rel(jobDirAbs, fullAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		Error(w, pkgerrors.NotFound("artifact"))
		return
	}

	fi, err := os.Stat(fullAbs)
	if err != nil || fi.IsDir() {
		Error(w, pkgerrors.NotFound("artifact"))
		return
	}

	ct := "application/octet-stream"
	switch strings.ToLower(filepath.Ext(fullAbs)) {
	case ".png":
		ct = "image/png"
	case ".jpg", ".jpeg":
		ct = "image/jpeg"
	case ".webp":
		ct = "image/webp"
	case ".gif":
		ct = "image/gif"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=86400")

	if r.Method == http.MethodHead {
		w.Header().Set("Content-Length", strconv.FormatInt(fi.Size(), 10))
		w.WriteHeader(http.StatusOK)
		return
	}

	http.ServeFile(w, r, fullAbs)
}
