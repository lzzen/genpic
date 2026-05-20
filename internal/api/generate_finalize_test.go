package api

import (
	"context"
	"testing"
	"time"

	"genpic/internal/jobstore"
	pkgerrors "genpic/pkg/errors"
)

func TestFinalizeJobResultAPIErrErrorCode(t *testing.T) {
	ctx := context.Background()
	st := jobstore.NewMemory(ctx, 24*time.Hour)
	SetJobStore(st)
	t.Cleanup(func() { SetJobStore(nil) })

	id, err := st.Create(&jobstore.Job{
		Model: "m", Provider: "p", Prompt: "x",
		Status: jobstore.StatusQueued,
	})
	if err != nil {
		t.Fatal(err)
	}
	finalizeJobResult(id, nil, pkgerrors.UpstreamTimeout())
	j, ok := st.Get(id)
	if !ok {
		t.Fatal("job missing")
	}
	if j.ErrorCode != "upstream_error/upstream_timeout" {
		t.Fatalf("error_code=%q", j.ErrorCode)
	}
	if j.Status != jobstore.StatusFailed {
		t.Fatalf("status=%s", j.Status)
	}
}

func TestFinalizeJobResult_preservesRequestedModelAndStoresEffective(t *testing.T) {
	ctx := context.Background()
	st := jobstore.NewMemory(ctx, 24*time.Hour)
	SetJobStore(st)
	t.Cleanup(func() { SetJobStore(nil) })

	id, err := st.Create(&jobstore.Job{
		Model: "xiangyun/auto", Provider: "xiangyun", Prompt: "cat",
		Status: jobstore.StatusQueued,
	})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]any{
		"data": []imageData{{URL: "https://example.com/x.png"}},
		"x_effective_provider":        "gemini",
		"x_effective_upstream_model":    "gemini-2.5-flash-image",
		"x_effective_model":           "gemini/gemini-2.5-flash-image",
	}
	finalizeJobResult(id, out, nil)
	j, ok := st.Get(id)
	if !ok {
		t.Fatal("job missing")
	}
	if j.Model != "xiangyun/auto" {
		t.Fatalf("model=%q want xiangyun/auto", j.Model)
	}
	if j.Provider != "xiangyun" {
		t.Fatalf("provider=%q want xiangyun", j.Provider)
	}
	if j.EffectiveModel != "gemini-2.5-flash-image" {
		t.Fatalf("effective_model=%q", j.EffectiveModel)
	}
	if j.EffectiveProvider != "gemini" {
		t.Fatalf("effective_provider=%q", j.EffectiveProvider)
	}
}
