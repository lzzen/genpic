package jobstore

import (
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const (
	granularityDay  = "day"
	granularityHour = "hour"
)

func normalizeGranularity(g string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(g)) {
	case "", granularityDay:
		return granularityDay, nil
	case granularityHour:
		return granularityHour, nil
	default:
		return "", fmt.Errorf("jobstore: invalid granularity %q (use day or hour)", g)
	}
}

func modelStatKey(model, provider string) string {
	return strings.TrimSpace(model) + "\x00" + strings.TrimSpace(provider)
}

func finishModelStatRow(m *ModelStatRow) {
	m.Total = m.Succeeded + m.Failed
	if m.Total > 0 {
		m.SuccessRate = float64(m.Succeeded) / float64(m.Total)
	}
	if m.Total < 3 {
		m.SampleInsufficient = true
	}
	if len(m.FailuresByCode) == 0 {
		m.FailuresByCode = nil
	}
}

func sortModelStatRows(rows []ModelStatRow) {
	sort.Slice(rows, func(i, j int) bool {
		ai, aj := rows[i].SuccessRate, rows[j].SuccessRate
		if ai != aj {
			return ai < aj
		}
		if rows[i].Failed != rows[j].Failed {
			return rows[i].Failed > rows[j].Failed
		}
		return rows[i].Model < rows[j].Model
	})
}

func percentile95MS(lat []float64) *int64 {
	if len(lat) < 5 {
		return nil
	}
	cp := append([]float64(nil), lat...)
	sort.Float64s(cp)
	idx := int(math.Ceil(0.95*float64(len(cp)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(cp) {
		idx = len(cp) - 1
	}
	v := int64(math.Round(cp[idx]))
	return &v
}

func avgFloat(lat []float64) *int64 {
	if len(lat) == 0 {
		return nil
	}
	var s float64
	for _, x := range lat {
		s += x
	}
	v := int64(math.Round(s / float64(len(lat))))
	return &v
}

func wallDayStart(t time.Time) time.Time {
	t = t.In(time.Local)
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}

func wallHourStart(t time.Time) time.Time {
	t = t.In(time.Local)
	y, mo, d := t.Date()
	return time.Date(y, mo, d, t.Hour(), 0, 0, 0, time.Local)
}

func generateBucketStarts(since, until time.Time, g string) []time.Time {
	if !until.After(since) {
		return nil
	}
	sLoc := since.In(time.Local)
	uLoc := until.In(time.Local)
	var out []time.Time
	switch g {
	case granularityHour:
		for t := wallHourStart(sLoc); t.Before(uLoc); t = t.Add(time.Hour) {
			te := t.Add(time.Hour)
			if te.After(since) && t.Before(until) {
				out = append(out, t)
			}
		}
	default:
		for t := wallDayStart(sLoc); t.Before(uLoc); t = t.Add(24 * time.Hour) {
			te := t.Add(24 * time.Hour)
			if te.After(since) && t.Before(until) {
				out = append(out, t)
			}
		}
	}
	return out
}

func bucketLabel(t time.Time, g string) string {
	t = t.In(time.Local)
	if g == granularityHour {
		return t.Format("2006-01-02 15:00")
	}
	return t.Format("2006-01-02")
}

func bucketEnd(t time.Time, g string) time.Time {
	if g == granularityHour {
		return t.Add(time.Hour)
	}
	return t.Add(24 * time.Hour)
}

// AdminModelStats for Memory store.
func (m *Memory) AdminModelStats(since, until time.Time) AdminModelStatsSummary {
	out := AdminModelStatsSummary{
		Since: since.Unix(),
		Until: until.Unix(),
	}
	if !until.After(since) {
		return out
	}
	type agg struct {
		succ, fail int64
		lat        []float64
		failCodes  map[string]int64
	}
	groups := map[string]*agg{}

	m.mu.Lock()
	for _, j := range m.ordered {
		if j.Status != StatusSucceeded && j.Status != StatusFailed {
			continue
		}
		if j.FinishedAt.IsZero() || j.FinishedAt.Before(since) || !j.FinishedAt.Before(until) {
			continue
		}
		mod := strings.TrimSpace(j.Model)
		prov := strings.TrimSpace(j.Provider)
		if prov == "" {
			prov = "(unknown)"
		}
		k := modelStatKey(mod, prov)
		a := groups[k]
		if a == nil {
			a = &agg{failCodes: map[string]int64{}}
			groups[k] = a
		}
		switch j.Status {
		case StatusSucceeded:
			a.succ++
		case StatusFailed:
			a.fail++
			ec := strings.TrimSpace(j.ErrorCode)
			if ec == "" {
				ec = "(empty)"
			}
			a.failCodes[ec]++
		}
		if !j.StartedAt.IsZero() && !j.FinishedAt.IsZero() {
			ms := float64(j.FinishedAt.Sub(j.StartedAt).Microseconds()) / 1000.0
			if ms >= 0 {
				a.lat = append(a.lat, ms)
			}
		}
	}
	m.mu.Unlock()

	for k, a := range groups {
		parts := strings.SplitN(k, "\x00", 2)
		mod, prov := parts[0], parts[1]
		if len(parts) < 2 {
			prov = "(unknown)"
		}
		row := ModelStatRow{
			Model:          mod,
			Provider:       prov,
			Succeeded:      a.succ,
			Failed:         a.fail,
			FailuresByCode: a.failCodes,
			AvgProcessingMs: avgFloat(a.lat),
			P95ProcessingMs: percentile95MS(a.lat),
		}
		finishModelStatRow(&row)
		out.Models = append(out.Models, row)
	}
	sortModelStatRows(out.Models)
	return out
}

// AdminModelStatsTimeseries for Memory store.
func (m *Memory) AdminModelStatsTimeseries(since, until time.Time, granularity string, modelFilter []string) AdminModelStatsTimeseries {
	g, err := normalizeGranularity(granularity)
	if err != nil {
		return AdminModelStatsTimeseries{Since: since.Unix(), Until: until.Unix(), Granularity: granularity}
	}
	out := AdminModelStatsTimeseries{
		Since:       since.Unix(),
		Until:       until.Unix(),
		Granularity: g,
	}
	if !until.After(since) {
		return out
	}
	filter := map[string]struct{}{}
	for _, id := range modelFilter {
		id = strings.TrimSpace(id)
		if id != "" {
			filter[id] = struct{}{}
		}
	}
	bucketStarts := generateBucketStarts(since, until, g)
	for _, bs := range bucketStarts {
		be := bucketEnd(bs, g)
		bucketSince := since
		if bs.After(bucketSince) {
			bucketSince = bs
		}
		bucketUntil := until
		if be.Before(bucketUntil) {
			bucketUntil = be
		}
		if !bucketUntil.After(bucketSince) {
			continue
		}
		sub := m.AdminModelStats(bucketSince, bucketUntil)
		rows := filterAndStripFailures(sub.Models, filter)
		out.Buckets = append(out.Buckets, ModelStatsBucket{
			Start:  bs.Unix(),
			End:    be.Unix(),
			Label:  bucketLabel(bs, g),
			Models: rows,
		})
	}
	return out
}

func filterAndStripFailures(rows []ModelStatRow, filter map[string]struct{}) []ModelStatRow {
	if len(filter) == 0 {
		out := make([]ModelStatRow, 0, len(rows))
		for i := range rows {
			r := rows[i]
			r.FailuresByCode = nil
			finishModelStatRow(&r)
			out = append(out, r)
		}
		sortModelStatRows(out)
		return out
	}
	out := make([]ModelStatRow, 0, len(filter))
	for i := range rows {
		if _, ok := filter[strings.TrimSpace(rows[i].Model)]; !ok {
			continue
		}
		r := rows[i]
		r.FailuresByCode = nil
		finishModelStatRow(&r)
		out = append(out, r)
	}
	sortModelStatRows(out)
	return out
}

// AdminModelStats for MySQL.
func (s *MySQL) AdminModelStats(since, until time.Time) AdminModelStatsSummary {
	out := AdminModelStatsSummary{
		Since: since.Unix(),
		Until: until.Unix(),
	}
	if !until.After(since) {
		return out
	}
	rows, err := s.db.Query(`
		SELECT model, provider,
			SUM(CASE WHEN status = 'succeeded' THEN 1 ELSE 0 END),
			SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END),
			AVG(CASE WHEN status IN ('succeeded','failed') AND started_at IS NOT NULL AND finished_at IS NOT NULL
				THEN TIMESTAMPDIFF(MICROSECOND, started_at, finished_at) / 1000.0 END)
		FROM generation_jobs
		WHERE status IN ('succeeded','failed')
		  AND finished_at >= ? AND finished_at < ?
		GROUP BY model, provider`,
		since, until,
	)
	if err != nil {
		return out
	}
	defer rows.Close()
	type key struct{ model, prov string }
	totals := map[key]*ModelStatRow{}
	for rows.Next() {
		var model, prov string
		var succ, fail int64
		var avg sql.NullFloat64
		if err := rows.Scan(&model, &prov, &succ, &fail, &avg); err != nil {
			continue
		}
		if strings.TrimSpace(prov) == "" {
			prov = "(unknown)"
		}
		k := key{model: strings.TrimSpace(model), prov: prov}
		row := &ModelStatRow{
			Model:          k.model,
			Provider:       prov,
			Succeeded:      succ,
			Failed:         fail,
			FailuresByCode: make(map[string]int64),
		}
		if avg.Valid {
			v := int64(math.Round(avg.Float64))
			row.AvgProcessingMs = &v
		}
		totals[k] = row
	}
	_ = rows.Close()

	frows, err := s.db.Query(`
		SELECT model, provider, COALESCE(NULLIF(TRIM(error_code), ''), '(empty)'), COUNT(*)
		FROM generation_jobs
		WHERE status = 'failed' AND finished_at >= ? AND finished_at < ?
		GROUP BY model, provider, error_code`,
		since, until,
	)
	if err == nil {
		defer frows.Close()
		for frows.Next() {
			var model, prov, code string
			var c int64
			if err := frows.Scan(&model, &prov, &code, &c); err != nil {
				continue
			}
			if strings.TrimSpace(prov) == "" {
				prov = "(unknown)"
			}
			k := key{model: strings.TrimSpace(model), prov: prov}
			if row, ok := totals[k]; ok {
				row.FailuresByCode[code] = c
			}
		}
	}

	for _, row := range totals {
		finishModelStatRow(row)
		out.Models = append(out.Models, *row)
	}
	s.attachModelLatencyP95(&out, since, until)
	sortModelStatRows(out.Models)
	return out
}

func (s *MySQL) attachModelLatencyP95(out *AdminModelStatsSummary, since, until time.Time) {
	rows, err := s.db.Query(`
		SELECT model, provider,
			TIMESTAMPDIFF(MICROSECOND, started_at, finished_at) / 1000.0
		FROM generation_jobs
		WHERE status IN ('succeeded','failed')
		  AND finished_at >= ? AND finished_at < ?
		  AND started_at IS NOT NULL AND finished_at IS NOT NULL`,
		since, until,
	)
	if err != nil {
		return
	}
	defer rows.Close()
	type key struct{ model, prov string }
	lat := map[key][]float64{}
	for rows.Next() {
		var model, prov string
		var ms float64
		if err := rows.Scan(&model, &prov, &ms); err != nil {
			continue
		}
		if strings.TrimSpace(prov) == "" {
			prov = "(unknown)"
		}
		k := key{model: strings.TrimSpace(model), prov: prov}
		if ms >= 0 {
			lat[k] = append(lat[k], ms)
		}
	}
	for i := range out.Models {
		k := key{model: out.Models[i].Model, prov: out.Models[i].Provider}
		if lats := lat[k]; len(lats) >= 5 {
			out.Models[i].P95ProcessingMs = percentile95MS(lats)
		}
	}
}

// AdminModelStatsTimeseries for MySQL.
func (s *MySQL) AdminModelStatsTimeseries(since, until time.Time, granularity string, modelFilter []string) AdminModelStatsTimeseries {
	g, err := normalizeGranularity(granularity)
	if err != nil {
		return AdminModelStatsTimeseries{Since: since.Unix(), Until: until.Unix(), Granularity: strings.TrimSpace(granularity)}
	}
	out := AdminModelStatsTimeseries{
		Since:       since.Unix(),
		Until:       until.Unix(),
		Granularity: g,
	}
	if !until.After(since) {
		return out
	}
	var dateExpr string
	switch g {
	case granularityHour:
		dateExpr = `DATE_FORMAT(finished_at, '%Y-%m-%d %H:00:00')`
	default:
		dateExpr = `DATE_FORMAT(finished_at, '%Y-%m-%d')`
	}
	args := []any{since, until}
	var modelWhere string
	var cleanModels []string
	for _, id := range modelFilter {
		id = strings.TrimSpace(id)
		if id != "" {
			cleanModels = append(cleanModels, id)
		}
	}
	if len(cleanModels) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(cleanModels)), ",")
		modelWhere = " AND model IN (" + placeholders + ")"
		for _, id := range cleanModels {
			args = append(args, id)
		}
	}
	q := fmt.Sprintf(`
		SELECT %s AS bucket_key, model, provider,
			SUM(CASE WHEN status = 'succeeded' THEN 1 ELSE 0 END),
			SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END),
			AVG(CASE WHEN status IN ('succeeded','failed') AND started_at IS NOT NULL AND finished_at IS NOT NULL
				THEN TIMESTAMPDIFF(MICROSECOND, started_at, finished_at) / 1000.0 END)
		FROM generation_jobs
		WHERE status IN ('succeeded','failed')
		  AND finished_at >= ? AND finished_at < ? %s
		GROUP BY bucket_key, model, provider
		ORDER BY bucket_key, model`, dateExpr, modelWhere)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return out
	}
	defer rows.Close()

	type rowKey struct {
		bucket, model, prov string
	}
	cell := map[rowKey]*ModelStatRow{}
	var bucketOrder []string
	seen := map[string]struct{}{}
	for rows.Next() {
		var bk, model, prov string
		var succ, fail int64
		var avg sql.NullFloat64
		if err := rows.Scan(&bk, &model, &prov, &succ, &fail, &avg); err != nil {
			continue
		}
		if strings.TrimSpace(prov) == "" {
			prov = "(unknown)"
		}
		if _, ok := seen[bk]; !ok {
			seen[bk] = struct{}{}
			bucketOrder = append(bucketOrder, bk)
		}
		rk := rowKey{bucket: bk, model: strings.TrimSpace(model), prov: prov}
		row := &ModelStatRow{
			Model:     rk.model,
			Provider:  prov,
			Succeeded: succ,
			Failed:    fail,
		}
		if avg.Valid {
			v := int64(math.Round(avg.Float64))
			row.AvgProcessingMs = &v
		}
		finishModelStatRow(row)
		cell[rk] = row
	}

	parseBucketTime := func(bk string) (time.Time, error) {
		bk = strings.TrimSpace(bk)
		if g == granularityHour {
			return time.ParseInLocation("2006-01-02 15:04:05", bk, time.Local)
		}
		return time.ParseInLocation("2006-01-02", bk, time.Local)
	}

	for _, bk := range bucketOrder {
		bt, err := parseBucketTime(bk)
		if err != nil {
			continue
		}
		var be time.Time
		if g == granularityHour {
			be = bt.Add(time.Hour)
		} else {
			be = bt.Add(24 * time.Hour)
		}
		var models []ModelStatRow
		for rk, row := range cell {
			if rk.bucket != bk {
				continue
			}
			r := *row
			r.FailuresByCode = nil
			models = append(models, r)
		}
		sortModelStatRows(models)
		out.Buckets = append(out.Buckets, ModelStatsBucket{
			Start:  bt.Unix(),
			End:    be.Unix(),
			Label:  bk,
			Models: models,
		})
	}
	return out
}
