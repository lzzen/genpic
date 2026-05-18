package mvpconfig

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadMissing(t *testing.T) {
	cfg, err := Read(filepath.Join(t.TempDir(), "none.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Found || cfg.MvpLitePort != "" || cfg.ServerPort != "" || cfg.DefaultBaseURL != "" || cfg.ModelIDMap != nil {
		t.Fatalf("want missing empty, got %#v", cfg)
	}
}

func TestReadOK(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
mvp_lite:
  port: "9090"
  default_base_url: "https://agg.example.com"
server:
  port: "7070"
model_id_map:
  "openai/gpt-image-2": "gpt-image-2-all"
  gpt-image-2: "gpt-image-2-all"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Read(path)
	if err != nil || !cfg.Found {
		t.Fatalf("err=%v found=%v", err, cfg.Found)
	}
	if cfg.MvpLitePort != "9090" || cfg.DefaultBaseURL != "https://agg.example.com" {
		t.Fatalf("mvp_lite: port=%q base=%q", cfg.MvpLitePort, cfg.DefaultBaseURL)
	}
	if cfg.ServerPort != "7070" {
		t.Fatalf("server.port: got %q", cfg.ServerPort)
	}
	if len(cfg.ModelIDMap) != 2 || cfg.ModelIDMap["gpt-image-2"] != "gpt-image-2-all" {
		t.Fatalf("model_id_map: %#v", cfg.ModelIDMap)
	}
}

func TestReadGeminiImageSize4KModelMap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
gemini:
  base_url: "https://g.example"
  image_size_4k_model_map:
    "gemini/gemini-3.1-flash-image-preview": "banana-2-4K"
    gemini-3-pro-image-preview: "gemini/banana-pro-4K"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Read(path)
	if err != nil || !cfg.Found {
		t.Fatalf("err=%v found=%v", err, cfg.Found)
	}
	m := cfg.GeminiImageSize4KModelMap
	if len(m) != 2 {
		t.Fatalf("map len: %#v", m)
	}
	if m["gemini/gemini-3.1-flash-image-preview"] != "gemini/banana-2-4K" {
		t.Fatalf("flash: %#v", m)
	}
	if m["gemini/gemini-3-pro-image-preview"] != "gemini/banana-pro-4K" {
		t.Fatalf("pro: %#v", m)
	}
}

func TestReadObjectStorage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
object_storage:
  enabled: true
  endpoint: "https://cos.example.com"
  region: "ap-shanghai"
  bucket: "b1"
  access_key: "ak"
  secret_key: "sk"
  public_base_url: "https://cdn.example.com"
  key_prefix: "p"
  use_path_style: true
  artifact_mode: "both"
  max_fetch_bytes: 1000
  fetch_timeout: "30s"
  url_fetch_hosts:
    - "oaiusercontent.com"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Read(path)
	if err != nil || !cfg.Found {
		t.Fatalf("err=%v found=%v", err, cfg.Found)
	}
	o := cfg.ObjectStorage
	if !o.Enabled || o.Bucket != "b1" || o.AccessKey != "ak" || o.ArtifactMode != "both" {
		t.Fatalf("object_storage: %#v", o)
	}
	if o.MaxFetchBytes != 1000 || o.FetchTimeout != 30*time.Second {
		t.Fatalf("limits: max=%d timeout=%s", o.MaxFetchBytes, o.FetchTimeout)
	}
	if len(o.URLFetchHosts) != 1 || o.URLFetchHosts[0] != "oaiusercontent.com" {
		t.Fatalf("hosts: %#v", o.URLFetchHosts)
	}
}

func TestObjectStorageDisabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
object_storage:
  enabled: false
  bucket: "ignored"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Read(path)
	if err != nil || !cfg.Found {
		t.Fatalf("read: err=%v found=%v", err, cfg.Found)
	}
	if cfg.ObjectStorage.Enabled {
		t.Fatalf("want disabled, got %#v", cfg.ObjectStorage)
	}
}

func TestObjectStorageEnabledDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
object_storage:
  enabled: true
  bucket: "mybucket"
  access_key: "k"
  secret_key: "s"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	o := cfg.ObjectStorage
	if o.ArtifactMode != "oss" {
		t.Fatalf("default artifact_mode: got %q", o.ArtifactMode)
	}
	if o.MaxFetchBytes != 25<<20 {
		t.Fatalf("default max_fetch_bytes: got %d", o.MaxFetchBytes)
	}
	if o.FetchTimeout != 60*time.Second {
		t.Fatalf("default fetch_timeout: got %s", o.FetchTimeout)
	}
	if len(o.URLFetchHosts) != 0 {
		t.Fatalf("want empty url_fetch_hosts: %#v", o.URLFetchHosts)
	}
}

func TestObjectStorageEnvOverrides(t *testing.T) {
	t.Setenv("GENPIC_OBJECT_STORAGE_ACCESS_KEY", "env-ak")
	t.Setenv("GENPIC_OBJECT_STORAGE_SECRET_KEY", "env-sk")
	t.Setenv("GENPIC_OBJECT_STORAGE_ENDPOINT", "https://oss-env.example.com")
	t.Setenv("GENPIC_OBJECT_STORAGE_REGION", "oss-cn-hangzhou")
	t.Setenv("GENPIC_OBJECT_STORAGE_BUCKET", "env-bucket")
	t.Setenv("GENPIC_OBJECT_STORAGE_PUBLIC_BASE_URL", "https://cdn-env.example.com")

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
object_storage:
  enabled: true
  endpoint: "https://yaml-ignored.example.com"
  region: "yaml-region"
  bucket: "yaml-bucket"
  access_key: "yaml-ak"
  secret_key: "yaml-sk"
  public_base_url: "https://yaml-cdn.example.com"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	o := cfg.ObjectStorage
	if o.AccessKey != "env-ak" || o.SecretKey != "env-sk" {
		t.Fatalf("keys: %#v", o)
	}
	if o.Endpoint != "https://oss-env.example.com" || o.Region != "oss-cn-hangzhou" || o.Bucket != "env-bucket" {
		t.Fatalf("endpoint/region/bucket: %#v", o)
	}
	if o.PublicBaseURL != "https://cdn-env.example.com" {
		t.Fatalf("public_base_url: got %q", o.PublicBaseURL)
	}
}
