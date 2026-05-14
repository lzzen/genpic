// Package api — auth HTTP handlers for POST /api/auth/* and GET|PUT /api/user/settings.
//
// All routes are unauthenticated at the transport layer; authentication is
// enforced by checking the session cookie within each handler. The session
// cookie (genpic_session) is HTTP-only and SameSite=Lax.
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"genpic/internal/auth"
	pkgerrors "genpic/pkg/errors"
)

// authStoreInstance is set by SetAuthStore during server startup.
// When nil (e.g. no database configured), all auth handlers return 503.
var authStoreInstance *auth.Store

// SetAuthStore wires the auth store into the API layer.
func SetAuthStore(s *auth.Store) { authStoreInstance = s }

// GetAuthStore returns the current auth store and true, or (nil, false) when none is set.
func GetAuthStore() (*auth.Store, bool) {
	if authStoreInstance == nil {
		return nil, false
	}
	return authStoreInstance, true
}

// CurrentUser returns the authenticated user from the auth store via the
// session cookie, or nil if unauthenticated or no store is available.
func CurrentUser(r *http.Request) *auth.User {
	return auth.UserFromContext(r.Context())
}

// ── Register ─────────────────────────────────────────────────────────────────

type registerRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

// HandleRegister serves POST /api/auth/register.
func HandleRegister(w http.ResponseWriter, r *http.Request) {
	if authStoreInstance == nil {
		Error(w, pkgerrors.New(http.StatusServiceUnavailable, pkgerrors.TypeInternal, "auth_disabled", "auth not available (no database configured)"))
		return
	}
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, pkgerrors.BadRequest("parse_error", "could not parse request body"))
		return
	}
	req.Email = strings.TrimSpace(req.Email)
	req.Password = strings.TrimSpace(req.Password)
	if req.Email == "" || req.Password == "" {
		Error(w, pkgerrors.BadRequest("missing_field", "email and password are required"))
		return
	}
	if len(req.Password) < 8 {
		Error(w, pkgerrors.BadRequest("password_too_short", "password must be at least 8 characters"))
		return
	}

	user, err := authStoreInstance.Register(req.Email, req.Password, req.DisplayName)
	if errors.Is(err, auth.ErrEmailTaken) {
		Error(w, pkgerrors.New(http.StatusConflict, pkgerrors.TypeValidation, "email_taken", "this email is already registered"))
		return
	}
	if err != nil {
		Error(w, pkgerrors.New(http.StatusInternalServerError, pkgerrors.TypeInternal, "register_error", err.Error()))
		return
	}

	token, err := authStoreInstance.CreateSession(user.ID)
	if err != nil {
		Error(w, pkgerrors.New(http.StatusInternalServerError, pkgerrors.TypeInternal, "session_error", "could not create session"))
		return
	}

	setSessionCookie(w, token)
	// Migrate any pre-login anonymous jobs if the client sends the session header.
	if sess := strings.TrimSpace(r.Header.Get(hdrGenpicSession)); sess != "" {
		authStoreInstance.MigrateAnonymousJobs(user.ID, sess)
	}
	JSON(w, http.StatusCreated, userResponse(user))
}

// ── Login ─────────────────────────────────────────────────────────────────────

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// HandleLogin serves POST /api/auth/login.
func HandleLogin(w http.ResponseWriter, r *http.Request) {
	if authStoreInstance == nil {
		Error(w, pkgerrors.New(http.StatusServiceUnavailable, pkgerrors.TypeInternal, "auth_disabled", "auth not available"))
		return
	}
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, pkgerrors.BadRequest("parse_error", "could not parse request body"))
		return
	}

	user, err := authStoreInstance.Login(req.Email, req.Password)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		Error(w, pkgerrors.New(http.StatusUnauthorized, pkgerrors.TypeAuthentication, "invalid_credentials", "invalid email or password"))
		return
	}
	if err != nil {
		Error(w, pkgerrors.New(http.StatusInternalServerError, pkgerrors.TypeInternal, "login_error", err.Error()))
		return
	}

	token, err := authStoreInstance.CreateSession(user.ID)
	if err != nil {
		Error(w, pkgerrors.New(http.StatusInternalServerError, pkgerrors.TypeInternal, "session_error", "could not create session"))
		return
	}

	setSessionCookie(w, token)
	if sess := strings.TrimSpace(r.Header.Get(hdrGenpicSession)); sess != "" {
		authStoreInstance.MigrateAnonymousJobs(user.ID, sess)
	}
	JSON(w, http.StatusOK, userResponse(user))
}

// ── Logout ────────────────────────────────────────────────────────────────────

// HandleLogout serves POST /api/auth/logout.
func HandleLogout(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(auth.SessionCookie)
	if err == nil && c.Value != "" && authStoreInstance != nil {
		authStoreInstance.DeleteSession(c.Value)
	}
	clearSessionCookie(w)
	JSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

// ── Me ────────────────────────────────────────────────────────────────────────

// HandleMe serves GET /api/auth/me.
func HandleMe(w http.ResponseWriter, r *http.Request) {
	user := CurrentUser(r)
	if user == nil {
		Error(w, pkgerrors.New(http.StatusUnauthorized, pkgerrors.TypeAuthentication, "unauthenticated", "not logged in"))
		return
	}
	JSON(w, http.StatusOK, userResponse(user))
}

// ── User settings ─────────────────────────────────────────────────────────────

type userSettingsRequest struct {
	CommunityAutoPublic *bool `json:"community_auto_public"`
	PromptPublic        *bool `json:"prompt_public"`
}

// HandleGetSettings serves GET /api/user/settings.
func HandleGetSettings(w http.ResponseWriter, r *http.Request) {
	if authStoreInstance == nil {
		Error(w, pkgerrors.New(http.StatusServiceUnavailable, pkgerrors.TypeInternal, "auth_disabled", "auth not available"))
		return
	}
	user := CurrentUser(r)
	if user == nil {
		Error(w, pkgerrors.New(http.StatusUnauthorized, pkgerrors.TypeAuthentication, "unauthenticated", "login required"))
		return
	}
	st, err := authStoreInstance.GetSettings(user.ID)
	if err != nil {
		Error(w, pkgerrors.New(http.StatusInternalServerError, pkgerrors.TypeInternal, "settings_error", err.Error()))
		return
	}
	JSON(w, http.StatusOK, st)
}

// HandleUpdateSettings serves PUT /api/user/settings.
func HandleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	if authStoreInstance == nil {
		Error(w, pkgerrors.New(http.StatusServiceUnavailable, pkgerrors.TypeInternal, "auth_disabled", "auth not available"))
		return
	}
	user := CurrentUser(r)
	if user == nil {
		Error(w, pkgerrors.New(http.StatusUnauthorized, pkgerrors.TypeAuthentication, "unauthenticated", "login required"))
		return
	}

	var req userSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, pkgerrors.BadRequest("parse_error", "could not parse request body"))
		return
	}

	// Fetch current settings so we only update provided fields.
	current, err := authStoreInstance.GetSettings(user.ID)
	if err != nil {
		Error(w, pkgerrors.New(http.StatusInternalServerError, pkgerrors.TypeInternal, "settings_error", err.Error()))
		return
	}
	if req.CommunityAutoPublic != nil {
		current.CommunityAutoPublic = *req.CommunityAutoPublic
	}
	if req.PromptPublic != nil {
		current.PromptPublic = *req.PromptPublic
	}
	if err := authStoreInstance.UpdateSettings(current); err != nil {
		Error(w, pkgerrors.New(http.StatusInternalServerError, pkgerrors.TypeInternal, "settings_error", err.Error()))
		return
	}
	JSON(w, http.StatusOK, current)
}

// ── Cookie helpers ────────────────────────────────────────────────────────────

func setSessionCookie(w http.ResponseWriter, token string) {
	maxAge := int(auth.DefaultSessionTTL.Seconds())
	if authStoreInstance != nil {
		maxAge = int(authStoreInstance.SessionTTL().Seconds())
	}
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// ── Response helpers ──────────────────────────────────────────────────────────

type userResp struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
}

func userResponse(u *auth.User) userResp {
	return userResp{ID: u.ID, Email: u.Email, DisplayName: u.DisplayName}
}
