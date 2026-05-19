package api

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"time"
)

const materializeFetchErrMsgMax = 4000
const materializeFetchURLLogMax = 8000

func truncateMaterializeString(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "…(truncated)"
}

// recordMaterializeFetchFailure logs a failed remote image rehost and inserts a row when MySQL is available.
func recordMaterializeFetchFailure(ctx context.Context, db *sql.DB, jobID, userID string, imageIndex int, sourceURL string, fetchErr error) {
	msg := ""
	if fetchErr != nil {
		msg = fetchErr.Error()
	}
	msg = truncateMaterializeString(msg, materializeFetchErrMsgMax)
	urlLog := truncateMaterializeString(sourceURL, materializeFetchURLLogMax)

	slog.Default().Error("oss_materialize_remote_fetch_failed",
		"job_id", jobID,
		"user_id", userID,
		"image_index", imageIndex,
		"source_url", urlLog,
		"err", msg,
	)

	if db == nil {
		return
	}
	id, err := randomHex(16)
	if err != nil {
		slog.Default().Error("oss_materialize_fetch_failure_id", "err", err.Error())
		return
	}
	_, err = db.ExecContext(ctx, `
INSERT INTO materialize_fetch_failures (id, job_id, user_id, image_index, source_url, err_message, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, jobID, userID, imageIndex, urlLog, msg, time.Now().UTC())
	if err != nil {
		slog.Default().Error("oss_materialize_fetch_failure_db_insert",
			"job_id", jobID,
			"user_id", userID,
			"image_index", imageIndex,
			"db_err", err.Error(),
		)
	}
}

func isRehostableRemoteImageURL(u string) bool {
	u = strings.TrimSpace(u)
	if u == "" || strings.HasPrefix(u, "/api/artifacts/") {
		return false
	}
	low := strings.ToLower(u)
	return strings.HasPrefix(low, "https://") || strings.HasPrefix(low, "http://")
}
