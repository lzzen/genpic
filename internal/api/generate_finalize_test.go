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
