package api

import "testing"

func TestHostInAllowlist(t *testing.T) {
	allow := []string{"example.com", "OPENAI.COM"}
	if !hostInAllowlist("example.com", allow) {
		t.Fatal("expected match")
	}
	if !hostInAllowlist("openai.com", allow) {
		t.Fatal("expected case-insensitive match")
	}
	if hostInAllowlist("evil.com", allow) {
		t.Fatal("expected no match")
	}
}
