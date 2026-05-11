package mvpconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadMissing(t *testing.T) {
	port, base, found, err := Read(filepath.Join(t.TempDir(), "none.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if found || port != "" || base != "" {
		t.Fatalf("want missing empty, got found=%v port=%q base=%q", found, port, base)
	}
}

func TestReadOK(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
mvp_lite:
  port: "9090"
  default_base_url: "https://agg.example.com"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	port, base, found, err := Read(path)
	if err != nil || !found {
		t.Fatalf("err=%v found=%v", err, found)
	}
	if port != "9090" || base != "https://agg.example.com" {
		t.Fatalf("port=%q base=%q", port, base)
	}
}
