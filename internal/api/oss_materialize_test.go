package api

import "testing"

func TestEffectiveFetchHostsForRehost(t *testing.T) {
	t.Parallel()
	cfg := []string{"cdn.example.com"}
	u := "https://img.other.com/p.png"
	got, err := effectiveFetchHostsForRehost(u, cfg)
	if err != nil || len(got) != 1 || got[0] != "cdn.example.com" {
		t.Fatalf("configured list should win: %#v err=%v", got, err)
	}
	got, err = effectiveFetchHostsForRehost("https://img.other.com/p.png", nil)
	if err != nil || len(got) != 1 || got[0] != "img.other.com" {
		t.Fatalf("derive host: %#v err=%v", got, err)
	}
	got, err = effectiveFetchHostsForRehost("http://data.openapi.wang/x.png", nil)
	if err != nil || len(got) != 1 || got[0] != "data.openapi.wang" {
		t.Fatalf("derive host http: %#v err=%v", got, err)
	}
	if _, err := effectiveFetchHostsForRehost("ftp://x.com/y", nil); err == nil {
		t.Fatal("want error for non-http(s)")
	}
}
