package api

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"rosboard/internal/auth"
)

type requestSessionKey struct{}

type requestSession struct {
	Token    string
	Username string
}

func withRequestSession(ctx context.Context, session auth.Session) context.Context {
	return context.WithValue(ctx, requestSessionKey{}, requestSession{Token: session.Token, Username: session.Username})
}

func sessionFromRequest(request *http.Request) requestSession {
	session, _ := request.Context().Value(requestSessionKey{}).(requestSession)
	return session
}

func publicAPI(path, method string) bool {
	switch {
	case path == "/api/health" && method == http.MethodGet:
		return true
	case path == "/api/bootstrap" && method == http.MethodGet:
		return true
	case path == "/api/setup/admin" && method == http.MethodPost:
		return true
	case path == "/api/auth/login" && method == http.MethodPost:
		return true
	default:
		return false
	}
}

func (s *Server) phaseAllows(request *http.Request) (bool, error) {
	if s.auth == nil {
		return true, nil
	}
	complete, err := s.auth.OnboardingComplete(request.Context())
	if err != nil {
		return false, err
	}
	if complete {
		return true, nil
	}
	path := request.URL.Path
	return path == "/api/auth/logout" ||
		path == "/api/account" ||
		path == "/api/setup/complete" ||
		path == "/api/settings" ||
		strings.HasPrefix(path, "/api/settings/") ||
		path == "/api/devices" ||
		strings.HasPrefix(path, "/api/devices/") ||
		path == "/api/device-onboarding/sessions" ||
		strings.HasPrefix(path, "/api/device-onboarding/sessions/"), nil
}

func (s *Server) authenticateRequest(writer http.ResponseWriter, request *http.Request) (auth.Session, bool) {
	cookie, err := request.Cookie(auth.SessionCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		writeAPIError(writer, http.StatusUnauthorized, "authentication_required", "authentication required")
		return auth.Session{}, false
	}
	session, err := s.auth.Authenticate(request.Context(), cookie.Value)
	if err != nil {
		clearSessionCookie(writer, request)
		if errors.Is(err, auth.ErrInvalidSession) {
			writeAPIError(writer, http.StatusUnauthorized, "authentication_required", "authentication required")
		} else {
			writeAPIError(writer, http.StatusInternalServerError, "authentication_failed", "failed to authenticate session")
		}
		return auth.Session{}, false
	}
	if session.Renewed {
		setSessionCookie(writer, request, session)
	}
	return session, true
}

func (s *Server) serveAuthAPI(writer http.ResponseWriter, request *http.Request) bool {
	switch request.URL.Path {
	case "/api/bootstrap":
		s.serveBootstrap(writer, request)
		return true
	case "/api/setup/admin":
		s.serveSetupAdmin(writer, request)
		return true
	case "/api/auth/login":
		s.serveLogin(writer, request)
		return true
	case "/api/auth/logout":
		s.serveLogout(writer, request)
		return true
	case "/api/account":
		s.serveAccountCredentials(writer, request)
		return true
	case "/api/setup/complete":
		s.serveSetupComplete(writer, request)
		return true
	default:
		return false
	}
}

func (s *Server) serveBootstrap(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	if s.auth == nil {
		writeJSON(writer, http.StatusOK, map[string]any{"phase": "ready", "authenticated": true, "onboardingComplete": true})
		return
	}
	account, err := s.auth.Admin(request.Context())
	if errors.Is(err, auth.ErrAdminNotFound) {
		writeJSON(writer, http.StatusOK, map[string]any{"phase": "needs_admin", "authenticated": false, "onboardingComplete": false})
		return
	}
	if err != nil {
		writeAPIError(writer, http.StatusInternalServerError, "bootstrap_failed", "failed to load setup state")
		return
	}
	complete, err := s.auth.OnboardingComplete(request.Context())
	if err != nil {
		writeAPIError(writer, http.StatusInternalServerError, "bootstrap_failed", "failed to load setup state")
		return
	}
	authenticated := false
	username := ""
	if cookie, cookieErr := request.Cookie(auth.SessionCookieName); cookieErr == nil {
		if session, sessionErr := s.auth.Authenticate(request.Context(), cookie.Value); sessionErr == nil {
			authenticated = true
			username = session.Username
			if session.Renewed {
				setSessionCookie(writer, request, session)
			}
		} else {
			clearSessionCookie(writer, request)
		}
	}
	phase := "needs_login"
	if authenticated && complete {
		phase = "ready"
	} else if authenticated {
		phase = "needs_routeros"
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"phase": phase, "authenticated": authenticated, "onboardingComplete": complete,
		"username": username, "adminCreated": !account.CreatedAt.IsZero(),
	})
}

func (s *Server) serveSetupAdmin(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	if s.auth == nil {
		writeAPIError(writer, http.StatusServiceUnavailable, "authentication_unavailable", "authentication service unavailable")
		return
	}
	if _, err := s.auth.Admin(request.Context()); err == nil {
		writeAPIError(writer, http.StatusConflict, "admin_exists", "administrator already exists")
		return
	} else if !errors.Is(err, auth.ErrAdminNotFound) {
		writeAPIError(writer, http.StatusInternalServerError, "setup_failed", "failed to load administrator state")
		return
	}
	var payload struct {
		Username             string `json:"username"`
		Password             string `json:"password"`
		PasswordConfirmation string `json:"passwordConfirmation"`
	}
	if err := decodeJSONBody(writer, request, &payload); err != nil {
		return
	}
	session, err := s.auth.CreateAdmin(request.Context(), payload.Username, payload.Password, payload.PasswordConfirmation)
	if err != nil {
		if errors.Is(err, auth.ErrAdminExists) {
			writeAPIError(writer, http.StatusConflict, "admin_exists", "administrator already exists")
		} else {
			writeAPIError(writer, http.StatusBadRequest, "invalid_admin", err.Error())
		}
		return
	}
	setSessionCookie(writer, request, session)
	writeJSON(writer, http.StatusCreated, map[string]any{"ok": true, "phase": "needs_routeros", "username": session.Username})
}

func (s *Server) serveLogin(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	if s.auth == nil {
		writeAPIError(writer, http.StatusServiceUnavailable, "authentication_unavailable", "authentication service unavailable")
		return
	}
	if _, err := s.auth.Admin(request.Context()); errors.Is(err, auth.ErrAdminNotFound) {
		writeAPIError(writer, http.StatusConflict, "setup_required", "administrator setup required")
		return
	} else if err != nil {
		writeAPIError(writer, http.StatusInternalServerError, "login_failed", "failed to load administrator state")
		return
	}
	var payload struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSONBody(writer, request, &payload); err != nil {
		return
	}
	session, err := s.auth.Login(request.Context(), remoteIP(request), payload.Username, payload.Password)
	if err != nil {
		var loginErr *auth.LoginError
		if errors.As(err, &loginErr) && errors.Is(loginErr, auth.ErrRateLimited) {
			seconds := max(1, int(loginErr.RetryAfter.Round(time.Second)/time.Second))
			writer.Header().Set("Retry-After", strconv.Itoa(seconds))
			writeAPIError(writer, http.StatusTooManyRequests, "login_rate_limited", "too many login attempts; try again later")
			return
		}
		if errors.Is(err, auth.ErrInvalidCredentials) {
			writeAPIError(writer, http.StatusUnauthorized, "invalid_credentials", "username or password is incorrect")
			return
		}
		writeAPIError(writer, http.StatusInternalServerError, "login_failed", "failed to create login session")
		return
	}
	setSessionCookie(writer, request, session)
	complete, _ := s.auth.OnboardingComplete(request.Context())
	phase := "needs_routeros"
	if complete {
		phase = "ready"
	}
	writeJSON(writer, http.StatusOK, map[string]any{"ok": true, "phase": phase, "username": session.Username})
}

func (s *Server) serveLogout(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	session := sessionFromRequest(request)
	if s.auth != nil {
		_ = s.auth.Logout(request.Context(), session.Token)
	}
	clearSessionCookie(writer, request)
	writeJSON(writer, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) serveAccountCredentials(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPut {
		methodNotAllowed(writer, http.MethodPut)
		return
	}
	var payload struct {
		Username             string `json:"username"`
		Password             string `json:"password"`
		PasswordConfirmation string `json:"passwordConfirmation"`
	}
	if err := decodeJSONBody(writer, request, &payload); err != nil {
		return
	}
	username, err := s.auth.UpdateCredentials(request.Context(), payload.Username, payload.Password, payload.PasswordConfirmation)
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, "invalid_credentials_update", err.Error())
		return
	}
	clearSessionCookie(writer, request)
	writeJSON(writer, http.StatusOK, map[string]any{"ok": true, "username": username, "reauthenticate": true})
}

func (s *Server) serveSetupComplete(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var payload struct {
		SkipRouterOS bool `json:"skipRouterOS"`
	}
	if err := decodeJSONBody(writer, request, &payload); err != nil {
		return
	}
	if !payload.SkipRouterOS && len(s.configSnapshot().Devices) == 0 {
		writeAPIError(writer, http.StatusConflict, "routeros_required", "save a RouterOS device or explicitly skip setup")
		return
	}
	if err := s.auth.CompleteOnboarding(request.Context()); err != nil {
		writeAPIError(writer, http.StatusInternalServerError, "setup_completion_failed", "failed to complete setup")
		return
	}
	restarting := !payload.SkipRouterOS && s.restart != nil
	if !payload.SkipRouterOS {
		s.scheduleRestart()
	}
	writeJSON(writer, http.StatusOK, map[string]any{"ok": true, "phase": "ready", "restarting": restarting})
}

func setSessionCookie(writer http.ResponseWriter, request *http.Request, session auth.Session) {
	http.SetCookie(writer, &http.Cookie{
		Name: auth.SessionCookieName, Value: session.Token, Path: "/", HttpOnly: true,
		Secure: request.TLS != nil, SameSite: http.SameSiteStrictMode,
		Expires: session.ExpiresAt, MaxAge: int(auth.SessionLifetime / time.Second),
	})
}

func clearSessionCookie(writer http.ResponseWriter, request *http.Request) {
	http.SetCookie(writer, &http.Cookie{
		Name: auth.SessionCookieName, Value: "", Path: "/", HttpOnly: true,
		Secure: request.TLS != nil, SameSite: http.SameSiteStrictMode,
		Expires: time.Unix(1, 0), MaxAge: -1,
	})
}

func sameOriginWrite(request *http.Request) bool {
	if request.Method == http.MethodGet || request.Method == http.MethodHead || request.Method == http.MethodOptions {
		return true
	}
	if strings.EqualFold(request.Header.Get("Sec-Fetch-Site"), "cross-site") {
		return false
	}
	origin := strings.TrimSpace(request.Header.Get("Origin"))
	if origin == "" {
		return false
	}
	originScheme, originHost, originPort, ok := parseExactOrigin(origin, "")
	if !ok {
		return false
	}
	// URL.Scheme is populated from an absolute-form request target and is
	// therefore client-controlled. The direct connection's TLS state is the
	// trusted effective scheme; deployments terminating TLS upstream must use a
	// separately configured HTTPS listener rather than accepting a forwarded
	// scheme header here.
	effectiveScheme := "http"
	if request.TLS != nil {
		effectiveScheme = "https"
	}
	requestScheme, requestHost, requestPort, ok := parseExactOrigin("//"+strings.TrimSpace(request.Host), effectiveScheme)
	if !ok || originScheme != requestScheme || originPort != requestPort || !sameOriginHost(originHost, requestHost) {
		return false
	}
	return true
}

func parseExactOrigin(raw, defaultScheme string) (scheme, host, port string, ok bool) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Opaque != "" || parsed.User != nil || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", "", false
	}
	scheme = strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if scheme == "" {
		scheme = strings.ToLower(strings.TrimSpace(defaultScheme))
	}
	if scheme != "http" && scheme != "https" {
		return "", "", "", false
	}
	host = strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "" {
		return "", "", "", false
	}
	port = parsed.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	} else if value, err := strconv.Atoi(port); err != nil || value < 0 || value > 65535 {
		return "", "", "", false
	} else {
		port = strconv.Itoa(value)
	}
	return scheme, host, port, true
}

func sameOriginHost(left, right string) bool {
	if left == right {
		return true
	}
	leftIP := net.ParseIP(left)
	rightIP := net.ParseIP(right)
	return leftIP != nil && rightIP != nil && leftIP.Equal(rightIP)
}

func setSecurityHeaders(writer http.ResponseWriter) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("Referrer-Policy", "same-origin")
}

func remoteIP(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err == nil {
		return host
	}
	return request.RemoteAddr
}

func methodNotAllowed(writer http.ResponseWriter, allowed string) {
	writer.Header().Set("Allow", allowed)
	writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
}

func decodeJSONBody(writer http.ResponseWriter, request *http.Request, target any) error {
	request.Body = http.MaxBytesReader(writer, request.Body, 64<<10)
	decoder := json.NewDecoder(request.Body)
	if err := decoder.Decode(target); err != nil {
		writeAPIError(writer, http.StatusBadRequest, "invalid_json", "invalid json body")
		return err
	}
	return nil
}

func writeAPIError(writer http.ResponseWriter, status int, code, message string) {
	writeJSON(writer, status, map[string]string{"code": code, "error": message})
}
