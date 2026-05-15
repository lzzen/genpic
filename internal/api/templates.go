package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"unicode/utf8"

	"genpic/internal/auth"
	"genpic/internal/jobstore"
	"genpic/internal/templatestore"
	pkgerrors "genpic/pkg/errors"
)

var (
	templateStoreInstance templatestore.Store

	adminEmailMu sync.RWMutex
	adminEmails  map[string]struct{} // lowercased
)

// SetTemplateStore wires the template persistence layer (MySQL). Nil disables template APIs except empty list.
func SetTemplateStore(s templatestore.Store) { templateStoreInstance = s }

// SetAdminEmails configures operator accounts (matched by user email, case-insensitive).
func SetAdminEmails(emails []string) {
	m := make(map[string]struct{})
	for _, e := range emails {
		e = strings.ToLower(strings.TrimSpace(e))
		if e != "" {
			m[e] = struct{}{}
		}
	}
	adminEmailMu.Lock()
	adminEmails = m
	adminEmailMu.Unlock()
}

func isAdminUser(u *auth.User) bool {
	if u == nil {
		return false
	}
	adminEmailMu.RLock()
	defer adminEmailMu.RUnlock()
	if len(adminEmails) == 0 {
		return false
	}
	_, ok := adminEmails[strings.ToLower(strings.TrimSpace(u.Email))]
	return ok
}

const maxTemplateRefJSONBytes = 1_500_000

type templateRefIn struct {
	MIMEType string `json:"mime_type"`
	B64JSON  string `json:"b64_json"`
}

type createTemplateRequest struct {
	JobID           string          `json:"job_id"`
	Visibility      string          `json:"visibility"` // private | public
	Title           string          `json:"title"`
	ReferenceImages []templateRefIn `json:"reference_images,omitempty"`
}

// HandleListTemplates serves GET /api/templates?model=...
// Optional auth: logged-in users also see their private templates for the model.
// The model query should match the SPA model selector (often catalog id like openai/…); rows are
// keyed by wire-style primary_model (no vendor/ prefix), so the server matches both the raw
// query and its normalised form.
func HandleListTemplates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	model := strings.TrimSpace(r.URL.Query().Get("model"))
	if model == "" {
		Error(w, pkgerrors.BadRequest("missing_query", "model query parameter is required"))
		return
	}
	if templateStoreInstance == nil {
		JSON(w, http.StatusOK, map[string]any{"object": "template.list", "data": []any{}})
		return
	}
	uid := ""
	if u := CurrentUser(r); u != nil {
		uid = u.ID
	}
	ctx := r.Context()
	list, err := templateStoreInstance.ListForModel(ctx, model, normalizeModelID(model), uid, 50)
	if err != nil {
		Error(w, pkgerrors.New(http.StatusInternalServerError, pkgerrors.TypeInternal, "template_list_error", err.Error()))
		return
	}
	out := make([]map[string]any, 0, len(list))
	for i := range list {
		out = append(out, templateToJSON(&list[i]))
	}
	JSON(w, http.StatusOK, map[string]any{"object": "template.list", "data": out})
}

func templateToJSON(t *templatestore.Template) map[string]any {
	if t == nil {
		return nil
	}
	m := map[string]any{
		"id":               t.ID,
		"object":           "generation.template",
		"user_id":          t.UserID,
		"provider":         t.Provider,
		"visibility":       t.Visibility,
		"title":            t.Title,
		"primary_model":    t.PrimaryModel,
		"models":           t.Models,
		"prompt":           t.Prompt,
		"result_image_url": t.ResultImageURL,
		"created_at":       t.CreatedAt.Unix(),
	}
	if t.Params != nil {
		m["params"] = t.Params
	}
	if len(t.ReferenceImages) > 0 {
		m["reference_images"] = t.ReferenceImages
	}
	return m
}

// HandleCreateTemplate serves POST /api/templates — save a succeeded job as a template.
func HandleCreateTemplate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if templateStoreInstance == nil {
		Error(w, pkgerrors.New(http.StatusServiceUnavailable, pkgerrors.TypeInternal, "templates_disabled", "templates require a database"))
		return
	}
	user := CurrentUser(r)
	if user == nil {
		Error(w, pkgerrors.New(http.StatusUnauthorized, pkgerrors.TypeAuthentication, "unauthenticated", "login required"))
		return
	}
	if jobStoreInstance == nil {
		Error(w, pkgerrors.New(http.StatusServiceUnavailable, pkgerrors.TypeInternal, "not_ready", "job store not initialised"))
		return
	}

	var req createTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, pkgerrors.BadRequest("parse_error", "could not parse request body"))
		return
	}
	jobID := strings.TrimSpace(req.JobID)
	if jobID == "" {
		Error(w, pkgerrors.BadRequest("missing_field", "job_id is required"))
		return
	}
	j, ok := jobStoreInstance.Get(jobID)
	if !ok {
		Error(w, pkgerrors.NotFound("job"))
		return
	}
	if j.UserID == "" || j.UserID != user.ID {
		Error(w, pkgerrors.New(http.StatusForbidden, pkgerrors.TypePermission, "forbidden", "only the job owner can save templates from this job"))
		return
	}
	if j.Status != jobstore.StatusSucceeded {
		Error(w, pkgerrors.BadRequest("job_not_succeeded", "only succeeded jobs can be saved as templates"))
		return
	}
	vis := strings.ToLower(strings.TrimSpace(req.Visibility))
	if vis == "public" {
		if !isAdminUser(user) {
			Error(w, pkgerrors.New(http.StatusForbidden, pkgerrors.TypePermission, "forbidden", "only administrators can publish public templates"))
			return
		}
	} else {
		vis = "private"
	}

	title := strings.TrimSpace(req.Title)
	if utf8.RuneCountInString(title) > 200 {
		rs := []rune(title)
		title = string(rs[:200])
	}

	var refs []map[string]any
	for _, ri := range req.ReferenceImages {
		mt := strings.TrimSpace(ri.MIMEType)
		b64 := strings.TrimSpace(ri.B64JSON)
		if b64 == "" {
			continue
		}
		refs = append(refs, map[string]any{
			"mime_type": mt,
			"b64_json":  b64,
		})
	}
	if len(refs) > 0 {
		b, err := json.Marshal(refs)
		if err != nil {
			Error(w, pkgerrors.BadRequest("invalid_reference_images", err.Error()))
			return
		}
		if len(b) > maxTemplateRefJSONBytes {
			Error(w, pkgerrors.BadRequest("reference_images_too_large", "reference_images exceed storage limit"))
			return
		}
	}

	resultURL := ""
	if len(j.Images) > 0 {
		resultURL = strings.TrimSpace(j.Images[0].URL)
	}
	if resultURL == "" {
		Error(w, pkgerrors.BadRequest("no_result_image", "job has no image URL to use as template preview"))
		return
	}

	// Store wire-style model ids (no openai/… prefix) plus provider so the SPA can rebuild catalog ids.
	wireModel := normalizeModelID(j.Model)
	models := []string{wireModel}
	tpl := &templatestore.Template{
		UserID:          user.ID,
		SourceJobID:     jobID,
		Provider:        strings.TrimSpace(j.Provider),
		Visibility:      vis,
		Title:           title,
		PrimaryModel:    wireModel,
		Models:          models,
		Prompt:          j.Prompt,
		Params:          cloneJobParams(j.Params),
		ReferenceImages: refs,
		ResultImageURL:  resultURL,
	}
	if tpl.Params != nil {
		tpl.Params.Model = wireModel
	}
	if err := templateStoreInstance.Create(r.Context(), tpl); err != nil {
		if errors.Is(err, templatestore.ErrDuplicateSourceJob) {
			Error(w, pkgerrors.New(http.StatusConflict, pkgerrors.TypeValidation, "template_exists_for_job", "该作品已保存为模板，无需重复保存"))
			return
		}
		Error(w, pkgerrors.New(http.StatusInternalServerError, pkgerrors.TypeInternal, "template_create_error", err.Error()))
		return
	}
	JSON(w, http.StatusCreated, templateToJSON(tpl))
}

func cloneJobParams(p *jobstore.JobParams) *jobstore.JobParams {
	if p == nil {
		return nil
	}
	cp := *p
	if len(p.WanBboxList) > 0 {
		cp.WanBboxList = append([]jobstore.JobBBox(nil), p.WanBboxList...)
	}
	return &cp
}

// HandleDeleteTemplate serves DELETE /api/templates/{id}.
func HandleDeleteTemplate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", http.MethodDelete)
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if templateStoreInstance == nil {
		Error(w, pkgerrors.New(http.StatusServiceUnavailable, pkgerrors.TypeInternal, "templates_disabled", "templates require a database"))
		return
	}
	user := CurrentUser(r)
	if user == nil {
		Error(w, pkgerrors.New(http.StatusUnauthorized, pkgerrors.TypeAuthentication, "unauthenticated", "login required"))
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		Error(w, pkgerrors.BadRequest("missing_path_param", "id is required in path"))
		return
	}
	admin := isAdminUser(user)
	ok, err := templateStoreInstance.Delete(r.Context(), id, user.ID, admin)
	if err != nil {
		Error(w, pkgerrors.New(http.StatusInternalServerError, pkgerrors.TypeInternal, "template_delete_error", err.Error()))
		return
	}
	if !ok {
		Error(w, pkgerrors.NotFound("template"))
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "deleted", "id": id})
}
