package web

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"patylook/internal/core/domain"
	"patylook/internal/core/port"
)

// newPage builds the template root for the current request, consuming any pending flash message.
func (s *Server) newPage(r *http.Request, title, active string) pageData {
	p := pageData{Title: title, Active: active, Errors: map[string]string{}, Data: map[string]any{}}
	if pr := principalFrom(r.Context()); pr != nil {
		p.User = pr.User
		p.CSRF = pr.Session.CSRFToken
		p.Flash = s.svc.Auth.PopFlash(r.Context(), pr.Session)
	}
	return p
}

func (s *Server) view(w http.ResponseWriter, r *http.Request, page string, p pageData) {
	s.viewStatus(w, r, http.StatusOK, page, p)
}

func (s *Server) viewStatus(w http.ResponseWriter, r *http.Request, status int, page string, p pageData) {
	if err := s.tpl.render(w, status, page, p); err != nil {
		s.log.Error("renderizar", "page", page, "err", err)
		http.Error(w, "Erro interno", http.StatusInternalServerError)
	}
}

// redirect sends the user to dest, leaving a flash message to be shown there.
func (s *Server) redirect(w http.ResponseWriter, r *http.Request, dest string, kind domain.FlashKind, msg string) {
	if pr := principalFrom(r.Context()); pr != nil && msg != "" {
		if err := s.svc.Auth.SetFlash(r.Context(), pr.Session, kind, msg); err != nil {
			s.log.Warn("salvar mensagem flash", "err", err)
		}
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

func (s *Server) success(w http.ResponseWriter, r *http.Request, dest, msg string) {
	s.redirect(w, r, dest, domain.FlashSuccess, msg)
}

// actionError reports a failed action (toggle, delete...) back on the page the user came from.
func (s *Server) actionError(w http.ResponseWriter, r *http.Request, dest string, err error) {
	var ve *domain.ValidationError
	switch {
	case errors.As(err, &ve):
		s.redirect(w, r, dest, domain.FlashError, ve.Error())
	case errors.Is(err, domain.ErrInUse):
		s.redirect(w, r, dest, domain.FlashError, "Não é possível excluir: o registro está sendo usado por outros dados.")
	case errors.Is(err, domain.ErrConflict):
		s.redirect(w, r, dest, domain.FlashError, "O registro foi alterado por outra pessoa. Atualize a página e tente novamente.")
	case errors.Is(err, domain.ErrInvalidTransition):
		s.redirect(w, r, dest, domain.FlashError, "Esta mudança de status não é permitida.")
	default:
		s.fail(w, r, err)
	}
}

// fieldErrors extracts validation messages from err.
func fieldErrors(err error) (map[string]string, bool) {
	var ve *domain.ValidationError
	if errors.As(err, &ve) {
		return ve.Fields, true
	}
	return nil, false
}

// pathID parses the {id} path value.
func pathID(r *http.Request) (int, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	return id, err == nil && id > 0
}

func pageParam(r *http.Request) int {
	n, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if n < 1 {
		return 1
	}
	return n
}

// pager is the data for the pagination partial.
type pager struct {
	Page, Pages int
	Total       int
	PrevURL     string
	NextURL     string
}

func newPager(r *http.Request, page, pages, total int) pager {
	link := func(n int) string {
		q := url.Values{}
		for k, v := range r.URL.Query() {
			q[k] = v
		}
		q.Set("page", strconv.Itoa(n))
		return r.URL.Path + "?" + q.Encode()
	}
	pg := pager{Page: page, Pages: pages, Total: total}
	if page > 1 {
		pg.PrevURL = link(page - 1)
	}
	if page < pages {
		pg.NextURL = link(page + 1)
	}
	return pg
}

func pagerFor[T any](r *http.Request, p port.Page[T]) pager {
	return newPager(r, p.Page, p.Pages(), p.Total)
}

// form collects request values and the errors found while parsing them.
type form struct {
	r    *http.Request
	errs *domain.ValidationError
}

func newForm(r *http.Request) *form {
	_ = r.ParseForm()
	return &form{r: r, errs: domain.NewValidationError()}
}

func (f *form) str(key string) string { return strings.TrimSpace(f.r.PostFormValue(key)) }

func (f *form) integer(key string) int {
	n, _ := strconv.Atoi(f.str(key))
	return n
}

func (f *form) bad(field, label string) { f.errs.Add(field, label+" inválido") }

// intField parses a required integer, recording an error when it is not one.
func (f *form) intField(field, label string) int {
	n, err := strconv.Atoi(f.str(field))
	if err != nil {
		f.bad(field, label)
	}
	return n
}

// money parses a monetary value; an empty value counts as zero when optional.
func (f *form) money(field, label string, required bool) domain.Money {
	raw := f.str(field)
	if raw == "" && !required {
		return 0
	}
	m, err := domain.ParseMoney(raw)
	if err != nil {
		f.bad(field, label)
	}
	return m
}

func (f *form) percent(field, label string, required bool) domain.Percent {
	raw := f.str(field)
	if raw == "" && !required {
		return 0
	}
	p, err := domain.ParsePercent(raw)
	if err != nil {
		f.bad(field, label)
	}
	return p
}

// date parses an HTML date input (YYYY-MM-DD) in the given location. Empty yields nil.
func (f *form) date(field, label string, loc *time.Location) *time.Time {
	raw := f.str(field)
	if raw == "" {
		return nil
	}
	t, err := time.ParseInLocation("2006-01-02", raw, loc)
	if err != nil {
		f.bad(field, label)
		return nil
	}
	return &t
}

// err returns the parse errors, or nil when every value was understood.
func (f *form) err() error { return f.errs.OrNil() }
