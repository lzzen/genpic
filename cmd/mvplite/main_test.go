package main

import "testing"

func TestBuildGenerationsURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://example.com", "https://example.com/v1/images/generations"},
		{"https://example.com/", "https://example.com/v1/images/generations"},
		{"https://example.com/v1", "https://example.com/v1/images/generations"},
		{"https://example.com/v1/", "https://example.com/v1/images/generations"},
		{"https://example.com/v1/images/generations", "https://example.com/v1/images/generations"},
	}
	for _, c := range cases {
		got, err := buildGenerationsURL(c.in)
		if err != nil {
			t.Fatalf("%q: %v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("%q: got %q want %q", c.in, got, c.want)
		}
	}
}
