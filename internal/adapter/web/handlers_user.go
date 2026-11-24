package web

import (
	"net/http"

	"patylook/internal/core/domain"
)

func (s *Server) userList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	page, err := s.svc.Users.List(r.Context(), principalFrom(r.Context()).User, pageParam(r), q)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	p := s.newPage(r, "Usuários", "users")
	p.Data["Users"] = page.Items
	p.Data["Query"] = q
	p.Data["Pager"] = pagerFor(r, page)
	s.view(w, r, "user_list", p)
}

func (s *Server) userFormPage(w http.ResponseWriter, r *http.Request, status int, u *domain.User, errs map[string]string) {
	cats, err := s.svc.Users.Categories(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	title := "Novo usuário"
	if u.ID != 0 {
		title = "Editar usuário"
	}
	p := s.newPage(r, title, "users")
	p.Errors = orEmpty(errs)
	p.Data["Edited"] = u
	p.Data["Categories"] = cats
	s.viewStatus(w, r, status, "user_form", p)
}

func (s *Server) userNew(w http.ResponseWriter, r *http.Request) {
	s.userFormPage(w, r, http.StatusOK, &domain.User{IsActive: true}, nil)
}

func (s *Server) userEdit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, domain.ErrNotFound)
		return
	}
	u, err := s.svc.Users.Get(r.Context(), principalFrom(r.Context()).User, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.userFormPage(w, r, http.StatusOK, u, nil)
}

func userFromForm(f *form, id int) *domain.User {
	return &domain.User{
		ID:         id,
		Username:   f.str("username"),
		Name:       f.str("name"),
		Email:      f.str("email"),
		CategoryID: f.integer("user_category_id"),
	}
}

func (s *Server) userCreate(w http.ResponseWriter, r *http.Request) {
	f := newForm(r)
	u := userFromForm(f, 0)
	password := r.PostFormValue("password")
	if password != r.PostFormValue("confirm_password") {
		f.errs.Add("confirm_password", "A confirmação não confere com a senha")
	}
	err := f.err()
	if err == nil {
		err = s.svc.Users.Create(r.Context(), principalFrom(r.Context()).User, u, password)
	}
	if fields, ok := fieldErrors(err); ok {
		s.userFormPage(w, r, http.StatusUnprocessableEntity, u, fields)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.success(w, r, "/users", "Usuário cadastrado.")
}

func (s *Server) userUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, domain.ErrNotFound)
		return
	}
	f := newForm(r)
	u := userFromForm(f, id)
	err := s.svc.Users.Update(r.Context(), principalFrom(r.Context()).User, u)
	if fields, ok := fieldErrors(err); ok {
		s.userFormPage(w, r, http.StatusUnprocessableEntity, u, fields)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.success(w, r, "/users", "Usuário atualizado.")
}

func (s *Server) userSetActive(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, domain.ErrNotFound)
		return
	}
	active := r.PostFormValue("active") == "1"
	if err := s.svc.Users.SetActive(r.Context(), principalFrom(r.Context()).User, id, active); err != nil {
		s.actionError(w, r, "/users", err)
		return
	}
	msg := "Usuário desativado."
	if active {
		msg = "Usuário ativado."
	}
	s.success(w, r, "/users", msg)
}

func (s *Server) userResetPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, domain.ErrNotFound)
		return
	}
	password := r.PostFormValue("password")
	if password != r.PostFormValue("confirm_password") {
		s.redirect(w, r, "/users/"+r.PathValue("id")+"/edit", domain.FlashError, "A confirmação não confere com a nova senha.")
		return
	}
	dest := "/users/" + r.PathValue("id") + "/edit"
	if err := s.svc.Users.ResetPassword(r.Context(), principalFrom(r.Context()).User, id, password); err != nil {
		s.actionError(w, r, dest, err)
		return
	}
	s.success(w, r, "/users", "Senha redefinida. O usuário precisará entrar novamente.")
}
