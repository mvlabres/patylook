// Package web is the HTTP driving adapter: routing, middleware, handlers and HTML rendering.
package web

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"patylook/internal/core/domain"
	"patylook/internal/core/port"
)

// Services groups the inbound ports the handlers depend on.
type Services struct {
	Auth      port.AuthService
	Users     port.UserService
	Catalog   port.CatalogService
	Customers port.CustomerService
	Payments  port.PaymentTypeService
	Orders    port.OrderService
	Dashboard port.DashboardService
}

type Options struct {
	Assets       fs.FS // contains templates/ and static/
	Location     *time.Location
	CookieSecure bool
	Logger       *slog.Logger
}

type Server struct {
	svc  Services
	opts Options
	tpl  *templates
	log  *slog.Logger
}

func NewServer(svc Services, opts Options) (*Server, error) {
	tpl, err := loadTemplates(opts.Assets, opts.Location)
	if err != nil {
		return nil, err
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{svc: svc, opts: opts, tpl: tpl, log: logger}, nil
}

// Handler returns the fully wired HTTP handler.
func (s *Server) Handler() (http.Handler, error) {
	static, err := fs.Sub(s.opts.Assets, "static")
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", staticHandler(static)))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})

	// Public
	mux.HandleFunc("GET /login", s.loginForm)
	mux.HandleFunc("POST /login", s.loginSubmit)

	// Authenticated
	auth := func(h http.HandlerFunc) http.Handler { return s.requireAuth(h) }
	admin := func(h http.HandlerFunc) http.Handler { return s.requireAuth(s.requireAdmin(h)) }
	// Upload routes cap the request body before it is parsed (the CSRF check parses it).
	upload := func(h http.HandlerFunc) http.Handler { return limitBody(maxUploadBody, s.requireAuth(h)) }

	mux.Handle("POST /logout", auth(s.logout))
	mux.Handle("GET /{$}", auth(s.dashboard))

	mux.Handle("GET /products", auth(s.productList))
	mux.Handle("GET /products/new", auth(s.productNew))
	mux.Handle("POST /products", upload(s.productCreate))
	mux.Handle("GET /products/{id}/edit", auth(s.productEdit))
	mux.Handle("POST /products/{id}", auth(s.productUpdate))
	mux.Handle("POST /products/{id}/active", auth(s.productSetActive))
	mux.Handle("GET /products/{id}/prices", auth(s.priceList))
	mux.Handle("POST /products/{id}/prices", auth(s.priceCreate))
	mux.Handle("POST /products/{id}/images", upload(s.imageUpload))
	mux.Handle("POST /products/{id}/images/{imageId}/delete", auth(s.imageDelete))
	mux.Handle("GET /images/products/{folder}/{file}", auth(s.serveImage))
	mux.Handle("GET /showcase", auth(s.showcase))

	mux.Handle("GET /categories", auth(s.categoryList))
	mux.Handle("GET /categories/new", auth(s.categoryNew))
	mux.Handle("POST /categories", auth(s.categoryCreate))
	mux.Handle("GET /categories/{id}/edit", auth(s.categoryEdit))
	mux.Handle("POST /categories/{id}", auth(s.categoryUpdate))
	mux.Handle("POST /categories/{id}/delete", auth(s.categoryDelete))

	mux.Handle("GET /customers", auth(s.customerList))
	mux.Handle("GET /customers/new", auth(s.customerNew))
	mux.Handle("POST /customers", auth(s.customerCreate))
	mux.Handle("GET /customers/{id}/edit", auth(s.customerEdit))
	mux.Handle("POST /customers/{id}", auth(s.customerUpdate))
	mux.Handle("POST /customers/{id}/active", auth(s.customerSetActive))

	mux.Handle("GET /payment-types", auth(s.paymentTypeList))
	mux.Handle("GET /payment-types/new", auth(s.paymentTypeNew))
	mux.Handle("POST /payment-types", auth(s.paymentTypeCreate))
	mux.Handle("GET /payment-types/{id}/edit", auth(s.paymentTypeEdit))
	mux.Handle("POST /payment-types/{id}", auth(s.paymentTypeUpdate))
	mux.Handle("POST /payment-types/{id}/delete", auth(s.paymentTypeDelete))

	mux.Handle("GET /orders", auth(s.orderList))
	mux.Handle("GET /orders/new", auth(s.orderNew))
	mux.Handle("POST /orders", auth(s.orderCreate))
	mux.Handle("GET /orders/{id}", auth(s.orderShow))
	mux.Handle("POST /orders/{id}/status", auth(s.orderChangeStatus))

	mux.Handle("GET /users", admin(s.userList))
	mux.Handle("GET /users/new", admin(s.userNew))
	mux.Handle("POST /users", admin(s.userCreate))
	mux.Handle("GET /users/{id}/edit", admin(s.userEdit))
	mux.Handle("POST /users/{id}", admin(s.userUpdate))
	mux.Handle("POST /users/{id}/active", admin(s.userSetActive))
	mux.Handle("POST /users/{id}/password", admin(s.userResetPassword))

	mux.Handle("GET /profile", auth(s.profile))
	mux.Handle("POST /profile/password", auth(s.profileChangePassword))

	mux.Handle("/", http.HandlerFunc(s.notFound))

	// Outermost first.
	var h http.Handler = mux
	h = sameOrigin(h, s.forbiddenCrossOrigin)
	h = securityHeaders(h, s.opts.CookieSecure)
	h = s.accessLog(h)
	h = s.recoverPanic(h)
	return h, nil
}

func staticHandler(fsys fs.FS) http.Handler {
	files := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "" || strings.HasSuffix(r.URL.Path, "/") { // no directory listings
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=3600")
		files.ServeHTTP(w, r)
	})
}

// ---- error handling ----

// fail translates an error into a response. Validation errors are not handled here;
// handlers re-render their forms with them.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		s.errorPage(w, r, http.StatusNotFound, "Registro não encontrado", "O registro solicitado não existe.")
	case errors.Is(err, domain.ErrForbidden):
		s.errorPage(w, r, http.StatusForbidden, "Acesso negado", "Você não tem permissão para acessar esta página.")
	case errors.Is(err, domain.ErrUnauthenticated):
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	default:
		s.log.Error("erro interno", "method", r.Method, "path", r.URL.Path, "err", err)
		s.errorPage(w, r, http.StatusInternalServerError, "Erro interno", "Algo deu errado. Tente novamente em instantes.")
	}
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	s.errorPage(w, r, http.StatusNotFound, "Página não encontrada", "O endereço acessado não existe.")
}

func (s *Server) forbiddenCrossOrigin(w http.ResponseWriter, r *http.Request) {
	s.errorPage(w, r, http.StatusForbidden, "Requisição bloqueada", "A origem da requisição não é permitida.")
}

func (s *Server) errorPage(w http.ResponseWriter, r *http.Request, status int, title, message string) {
	p := s.newPage(r, title, "")
	p.Data["Status"] = status
	p.Data["Message"] = message
	if err := s.tpl.render(w, status, "error", p); err != nil {
		s.log.Error("renderizar página de erro", "err", err)
		http.Error(w, title, status)
	}
}

type ctxKey int

const principalKey ctxKey = 1

// principal is the authenticated caller attached to the request context.
type principal struct {
	User    *domain.User
	Session *domain.Session
	Token   string
}

func principalFrom(ctx context.Context) *principal {
	p, _ := ctx.Value(principalKey).(*principal)
	return p
}
