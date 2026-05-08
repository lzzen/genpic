package main

import (
	"testing"
)

func TestBuildGenerationsURL(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "bare origin",
			input: "https://api.example.com",
			want:  "https://api.example.com/v1/images/generations",
		},
		{
			name:  "origin with trailing slash",
			input: "https://api.example.com/",
			want:  "https://api.example.com/v1/images/generations",
		},
		{
			name:  "origin already has /v1",
			input: "https://api.example.com/v1",
			want:  "https://api.example.com/v1/images/generations",
		},
		{
			name:  "origin with /v1/",
			input: "https://api.example.com/v1/",
			want:  "https://api.example.com/v1/images/generations",
		},
		{
			name:    "empty base_url",
			input:   "",
			wantErr: true,
		},
		{
			name:    "non-http scheme",
			input:   "ftp://example.com",
			wantErr: true,
		},
		{
			name:    "invalid url",
			input:   "not-a-url",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildGenerationsURL(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("wantErr=%v got err=%v", tc.wantErr, err)
			}
			if !tc.wantErr && got != tc.want {
				t.Errorf("want %q got %q", tc.want, got)
			}
		})
	}
}

func TestUpstreamModelForOpenAIImages(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"openai/gpt-image-2", "gpt-image-2"},
		{"gpt-image-2", "gpt-image-2"},
		{"gemini/gemini-2.5-flash-image", "gemini-2.5-flash-image"},
		{"wan/wan2.7-image", "wan2.7-image"},
		{"  openai/gpt-image-2  ", "gpt-image-2"},
	}
	for _, tc := range cases {
		if got := upstreamModelForOpenAIImages(tc.in); got != tc.want {
			t.Errorf("upstreamModelForOpenAIImages(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
