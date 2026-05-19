package jobstore

import (
	"context"
	"testing"
	"time"
)

func TestMemoryAdminModelStatsWindow(t *testing.T) {
	ctx := context.Background()
	m := NewMemory(ctx, 24*time.Hour)
	now := time.Now().UTC()
	until := now
	since := until.Add(-48 * time.Hour)

	add := func(model, prov string, st Status, fin time.Time, start time.Time, ec string) {
		j := &Job{
			Model: model, Provider: prov, Prompt: "p",
			Status: st, ErrorCode: ec,
			CreatedAt: fin.Add(-time.Hour), StartedAt: start, FinishedAt: fin,
		}
		if _, err := m.Create(j); err != nil {
			t.Fatal(err)
		}
	}

	t0 := until.Add(-10 * time.Hour)
	add("openai/gpt-image-2", "openai", StatusSucceeded, t0, t0.Add(-2*time.Second), "")
	add("openai/gpt-image-2", "openai", StatusSucceeded, t0.Add(time.Hour), t0.Add(time.Hour).Add(-3*time.Second), "")
	add("openai/gpt-image-2", "openai", StatusFailed, t0.Add(2*time.Hour), t0.Add(2*time.Hour).Add(-time.Second), "generation_error")
	add("gemini/x", "gemini", StatusSucceeded, t0.Add(3*time.Hour), t0.Add(3*time.Hour).Add(-500*time.Millisecond), "")

	sum := m.AdminModelStats(since, until)
	if len(sum.Models) < 2 {
		t.Fatalf("expected at least 2 model rows, got %d", len(sum.Models))
	}
	var gpt *ModelStatRow
	for i := range sum.Models {
		if sum.Models[i].Model == "openai/gpt-image-2" {
			gpt = &sum.Models[i]
			break
		}
	}
	if gpt == nil {
		t.Fatal("missing openai aggregate")
	}
	if gpt.Succeeded != 2 || gpt.Failed != 1 || gpt.Total != 3 {
		t.Fatalf("gpt counts: %+v", gpt)
	}
	if gpt.AvgProcessingMs == nil || *gpt.AvgProcessingMs <= 0 {
		t.Fatalf("expected avg ms, got %v", gpt.AvgProcessingMs)
	}
	if len(gpt.FailuresByCode) == 0 {
		t.Fatal("expected failure codes")
	}

	ts := m.AdminModelStatsTimeseries(since, until, "day", nil)
	if len(ts.Buckets) == 0 {
		t.Fatal("expected day buckets")
	}
}
