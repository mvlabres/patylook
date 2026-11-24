package web

import (
	"net/http"

	"patylook/internal/core/domain"
)

func (s *Server) brandList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	page, err := s.svc.Brands.List(r.Context(), pageParam(r), q)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	p := s.newPage(r, "Marcas", "brands")
	p.Data["Brands"] = page.Items
	p.Data["Query"] = q
	p.Data["Pager"] = pagerFor(r, page)
	s.view(w, r, "brand_list", p)
}

func (s *Server) brandFormPage(w http.ResponseWriter, r *http.Request, status int, b *domain.Brand, errs map[string]string) {
	title := "Nova marca"
	if b.ID != 0 {
		title = "Editar marca"
	}
	p := s.newPage(r, title, "brands")
	p.Errors = orEmpty(errs)
	p.Data["Brand"] = b
	s.viewStatus(w, r, status, "brand_form", p)
}

func (s *Server) brandNew(w http.ResponseWriter, r *http.Request) {
	s.brandFormPage(w, r, http.StatusOK, &domain.Brand{IsActive: true}, nil)
}

func (s *Server) brandEdit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, domain.ErrNotFound)
		return
	}
	b, err := s.svc.Brands.Get(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.brandFormPage(w, r, http.StatusOK, b, nil)
}

func (s *Server) brandSave(w http.ResponseWriter, r *http.Request, id int, msg string) {
	f := newForm(r)
	b := &domain.Brand{ID: id, Name: f.str("name")}
	err := s.svc.Brands.Save(r.Context(), principalFrom(r.Context()).User, b)
	if fields, ok := fieldErrors(err); ok {
		s.brandFormPage(w, r, http.StatusUnprocessableEntity, b, fields)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.success(w, r, "/brands", msg)
}

func (s *Server) brandCreate(w http.ResponseWriter, r *http.Request) {
	s.brandSave(w, r, 0, "Marca cadastrada.")
}

func (s *Server) brandUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, domain.ErrNotFound)
		return
	}
	s.brandSave(w, r, id, "Marca atualizada.")
}

func (s *Server) brandSetActive(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, domain.ErrNotFound)
		return
	}
	active := r.PostFormValue("active") == "1"
	if err := s.svc.Brands.SetActive(r.Context(), id, active); err != nil {
		s.actionError(w, r, "/brands", err)
		return
	}
	msg := "Marca desativada."
	if active {
		msg = "Marca ativada."
	}
	s.success(w, r, "/brands", msg)
}
