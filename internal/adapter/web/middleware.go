package web

import (
	"context"
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"time"

	"patylook/internal/core/domain"
)

const sessionCookie = "patylook_session"

// recoverPanic turns panics into a 500 response instead of dropping the connection.
func (s *Server) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				s.log.Error("panic", "path", r.URL.Path, "panic", rec, "stack", string(debug.Stack()))
				s.errorPage(w, r, http.StatusInternalServerError, "Erro interno", "Algo deu errado. Tente novamente em instantes.")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.log.Info("request", "method", r.Method, "path", r.URL.Path, "status", rec.status, "duration", time.Since(start))
	})
}

func securityHeaders(next http.Handler, hsts bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; frame-ancestors 'none'; form-action 'self'; base-uri 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		if hsts {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

// sameOrigin rejects cross-site state-changing requests using the browser-supplied
// Sec-Fetch-Site / Origin headers. It complements the per-session CSRF token.
func sameOrigin(next http.Handler, deny http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
				if site != "same-origin" && site != "none" {
					deny(w, r)
					return
				}
			} else if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || !sameHost(u.Host, r.Host) {
					deny(w, r)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func sameHost(a, b string) bool { return strings.EqualFold(a, b) }

// requireAuth resolves the session cookie, enforces the CSRF token on unsafe methods
// and attaches the principal to the request context.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := ""
		if c, err := r.Cookie(sessionCookie); err == nil {
			token = c.Value
		}
		sess, user, err := s.svc.Auth.Authenticate(r.Context(), token)
		if err != nil {
			if errors.Is(err, domain.ErrUnauthenticated) {
				s.clearSessionCookie(w)
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}
			s.fail(w, r, err)
			return
		}

		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			// Multipart bodies are parsed here (once), so oversized uploads get a precise answer.
			if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
				if err := r.ParseMultipartForm(multipartMemory); err != nil {
					var tooBig *http.MaxBytesError
					if errors.As(err, &tooBig) {
						s.errorPage(w, r, http.StatusRequestEntityTooLarge, "Arquivos muito grandes",
							"O envio excede o tamanho máximo permitido. Reduza o número ou o tamanho das imagens.")
					} else {
						s.errorPage(w, r, http.StatusBadRequest, "Requisição inválida", "Não foi possível ler o formulário enviado.")
					}
					return
				}
			}
			got := r.PostFormValue("csrf_token")
			if got == "" {
				got = r.Header.Get("X-CSRF-Token")
			}
			if subtle.ConstantTimeCompare([]byte(got), []byte(sess.CSRFToken)) != 1 {
				s.errorPage(w, r, http.StatusForbidden, "Sessão inválida", "O formulário expirou. Volte e tente novamente.")
				return
			}
		}

		ctx := context.WithValue(r.Context(), principalKey, &principal{User: user, Session: sess, Token: token})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := principalFrom(r.Context()); p == nil || !p.User.Admin {
			s.fail(w, r, domain.ErrForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.opts.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.opts.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

// clientKey identifies the caller for login throttling.
func clientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
