package modelmap

import "testing"

func TestApplyNilMap(t *testing.T) {
	if got := Apply(nil, []string{"a", "b"}, "def"); got != "def" {
		t.Fatalf("got %q", got)
	}
}

func TestApplyFirstMatch(t *testing.T) {
	m := map[string]string{
		"openai/gpt-image-2": "gpt-image-2-all",
		"gpt-image-2":        "should-not-win-if-catalog-first",
	}
	candidates := []string{"openai/gpt-image-2", "gpt-image-2", "gpt-image-2"}
	if got := Apply(m, candidates, "gpt-image-2"); got != "gpt-image-2-all" {
		t.Fatalf("got %q", got)
	}
}

func TestApplyFallbackToWire(t *testing.T) {
	m := map[string]string{"gpt-image-2": "gpt-image-2-all"}
	if got := Apply(m, []string{"openai/gpt-image-2", "gpt-image-2"}, "gpt-image-2"); got != "gpt-image-2-all" {
		t.Fatalf("got %q", got)
	}
}

func TestApplyNoKey(t *testing.T) {
	m := map[string]string{"other": "x"}
	if got := Apply(m, []string{"gpt-image-2"}, "gpt-image-2"); got != "gpt-image-2" {
		t.Fatalf("got %q", got)
	}
}

func TestApplyEmptyMappedValueIgnored(t *testing.T) {
	m := map[string]string{"gpt-image-2": "  "}
	if got := Apply(m, []string{"gpt-image-2"}, "gpt-image-2"); got != "gpt-image-2" {
		t.Fatalf("got %q", got)
	}
}
