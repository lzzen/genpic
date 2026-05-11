package compatctx

import (
	"strings"
	"testing"
)

func TestSanitizeForStderrLogJSON(t *testing.T) {
	longData := `"data":"` + strings.Repeat("A", 150) + `"`
	longThought := `"thoughtSignature":"` + strings.Repeat("B", 80) + `"`
	raw := `{"parts":[{` + longData + `,` + longThought + `}]}`
	out := SanitizeForStderrLogJSON([]byte(raw))
	if strings.Contains(out, strings.Repeat("A", 150)) {
		t.Error("expected base64 data to be redacted")
	}
	if strings.Contains(out, strings.Repeat("B", 80)) {
		t.Error("expected thoughtSignature to be redacted")
	}
	if !strings.Contains(out, "[omitted large base64]") {
		t.Errorf("missing base64 placeholder: %s", out)
	}
	if !strings.Contains(out, "[omitted long thoughtSignature]") {
		t.Errorf("missing thought placeholder: %s", out)
	}
}
