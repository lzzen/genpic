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
	if cfg.Found || cfg.MvpLitePort != "" || cfg.ServerPort != "" || cfg.DefaultBaseURL != "" {
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
}
