package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestFetchRemoteImage_httptestHTTP(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/img.png" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a})
	}))
	t.Cleanup(srv.Close)

	raw := srv.URL + "/img.png"
	pu, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	host := strings.ToLower(pu.Hostname())
	b, ct, err := FetchRemoteImage(context.Background(), raw, []string{host}, 1<<20, 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 8 || !strings.Contains(strings.ToLower(ct), "png") {
		t.Fatalf("body=%d ct=%q", len(b), ct)
	}
}

// TestIntegration_ThirdPartyRemoteImageFetch hits real upstream-model image URLs when
// GENPIC_TEST_REMOTE_IMAGE_FETCH=1 (default off: flaky, rate limits, expiring presignatures).
//
// Typical failure reasons: DNS/TLS/HTTP status != 200, body > max_fetch_bytes, timeout,
// resolved IP in private range, expired OSS query params, 403 from CDN.
func TestIntegration_ThirdPartyRemoteImageFetch(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	if os.Getenv("GENPIC_TEST_REMOTE_IMAGE_FETCH") != "1" {
		t.Skip("set GENPIC_TEST_REMOTE_IMAGE_FETCH=1 to run live URL fetches")
	}
	ctx := context.Background()
	maxB := int64(25 << 20)
	timeout := 60 * time.Second

	cases := []struct {
		name   string
		raw    string
		allow  []string
		minLen int
	}{
		{
			name: "openapi_wang_http_png",
			raw:  "http://data.openapi.wang/images/2622b815141ccd243569c765165a021f.png",
			allow: []string{"data.openapi.wang"},
		},
		{
			name: "dashscope_oss_presigned_https",
			raw: "https://dashscope-7c2c.oss-accelerate.aliyuncs.com/1d/b2/20260519/6e8aa136/53e56def-de1c-44c4-8e00-60ba08736ef8_0.png?Expires=1779241476&OSSAccessKeyId=LTAI5tPxpiCM2hjmWrFXrym1&Signature=CaGiaCoUVdAHAoHrqLMMXg2TxCY%3D",
			allow: []string{"dashscope-7c2c.oss-accelerate.aliyuncs.com"},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			minLen := tc.minLen
			if minLen == 0 {
				minLen = 32
			}
			b, _, err := FetchRemoteImage(ctx, tc.raw, tc.allow, maxB, timeout)
			if err != nil {
				t.Fatal(err)
			}
			if len(b) < minLen {
				t.Fatalf("short body: %d bytes", len(b))
			}
		})
	}
}

func TestIntegration_ThirdPartyRemoteImageDerivedAllowlist(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	if os.Getenv("GENPIC_TEST_REMOTE_IMAGE_FETCH") != "1" {
		t.Skip("set GENPIC_TEST_REMOTE_IMAGE_FETCH=1")
	}
	raw := "http://data.openapi.wang/images/2622b815141ccd243569c765165a021f.png"
	hosts, err := effectiveFetchHostsForRehost(raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := FetchRemoteImage(context.Background(), raw, hosts, 1<<20, 60*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 32 {
		t.Fatalf("short body: %d", len(b))
	}
}
