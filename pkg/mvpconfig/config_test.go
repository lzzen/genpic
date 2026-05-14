package mvpconfig

import (
	"os"
	"path/filepath"
	"testing"
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
