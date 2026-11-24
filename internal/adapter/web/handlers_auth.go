package web

import (
	"errors"
	"net/http"

	"patylook/internal/core/domain"
)

func (s *Server) loginForm(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if _, _, err := s.svc.Auth.Authenticate(r.Context(), c.Value); err == nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
	}
	p := s.newPage(r, "Entrar", "")
	p.Data["Username"] = ""
	s.view(w, r, "login", p)
}

func (s *Server) loginSubmit(w http.ResponseWriter, r *http.Request) {
	f := newForm(r)
	username := f.str("username")
	res, err := s.svc.Auth.Login(r.Context(), clientKey(r), username, r.PostFormValue("password"))
	if err != nil {
		status := http.StatusUnauthorized
		msg := domain.ErrInvalidCredentials.Error()
		switch {
		case errors.Is(err, domain.ErrTooManyAttempts):
			status, msg = http.StatusTooManyRequests, err.Error()
		case !errors.Is(err, domain.ErrInvalidCredentials):
			s.fail(w, r, err)
			return
		}
		p := s.newPage(r, "Entrar", "")
		p.Errors[""] = msg
		p.Data["Username"] = username
		s.viewStatus(w, r, status, "login", p)
		return
	}
	s.setSessionCookie(w, res.Token)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.Auth.Logout(r.Context(), principalFrom(r.Context()).Token); err != nil {
		s.log.Warn("encerrar sessão", "err", err)
	}
	s.clearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	d, err := s.svc.Dashboard.Load(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	p := s.newPage(r, "Painel", "dashboard")
	p.Data["Dashboard"] = d
	s.view(w, r, "dashboard", p)
}

func (s *Server) profile(w http.ResponseWriter, r *http.Request) {
	s.view(w, r, "profile", s.newPage(r, "Meu perfil", "profile"))
}

func (s *Server) profileChangePassword(w http.ResponseWriter, r *http.Request) {
	user := principalFrom(r.Context()).User
	f := newForm(r)
	if r.PostFormValue("new_password") != r.PostFormValue("confirm_password") {
		f.errs.Add("confirm_password", "A confirmação não confere com a nova senha")
	}
	err := f.err()
	if err == nil {
		err = s.svc.Users.ChangeOwnPassword(r.Context(), user, r.PostFormValue("current_password"), r.PostFormValue("new_password"))
	}
	if fields, ok := fieldErrors(err); ok {
		p := s.newPage(r, "Meu perfil", "profile")
		p.Errors = fields
		s.viewStatus(w, r, http.StatusUnprocessableEntity, "profile", p)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	// All sessions were revoked by the password change, including this one.
	s.clearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
