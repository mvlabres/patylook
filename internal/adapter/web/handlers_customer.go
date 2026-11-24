package web

import (
	"net/http"

	"patylook/internal/core/domain"
)

// ---- customers ----

func (s *Server) customerList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	page, err := s.svc.Customers.List(r.Context(), pageParam(r), q)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	p := s.newPage(r, "Clientes", "customers")
	p.Data["Customers"] = page.Items
	p.Data["Query"] = q
	p.Data["Pager"] = pagerFor(r, page)
	s.view(w, r, "customer_list", p)
}

func (s *Server) customerFormPage(w http.ResponseWriter, r *http.Request, status int, c *domain.Customer, errs map[string]string) {
	title := "Novo cliente"
	if c.ID != 0 {
		title = "Editar cliente"
	}
	p := s.newPage(r, title, "customers")
	p.Errors = orEmpty(errs)
	p.Data["Customer"] = c
	s.viewStatus(w, r, status, "customer_form", p)
}

func (s *Server) customerNew(w http.ResponseWriter, r *http.Request) {
	s.customerFormPage(w, r, http.StatusOK, &domain.Customer{IsActive: true}, nil)
}

func (s *Server) customerEdit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, domain.ErrNotFound)
		return
	}
	c, err := s.svc.Customers.Get(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.customerFormPage(w, r, http.StatusOK, c, nil)
}

func (s *Server) customerSave(w http.ResponseWriter, r *http.Request, id int, msg string) {
	f := newForm(r)
	c := &domain.Customer{
		ID: id, Name: f.str("name"), Document: f.str("document"), Phone: f.str("phone"), Email: f.str("email"),
	}
	err := s.svc.Customers.Save(r.Context(), c)
	if fields, ok := fieldErrors(err); ok {
		s.customerFormPage(w, r, http.StatusUnprocessableEntity, c, fields)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.success(w, r, "/customers", msg)
}

func (s *Server) customerCreate(w http.ResponseWriter, r *http.Request) {
	s.customerSave(w, r, 0, "Cliente cadastrado.")
}

func (s *Server) customerUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, domain.ErrNotFound)
		return
	}
	s.customerSave(w, r, id, "Cliente atualizado.")
}

func (s *Server) customerSetActive(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, domain.ErrNotFound)
		return
	}
	active := r.PostFormValue("active") == "1"
	if err := s.svc.Customers.SetActive(r.Context(), id, active); err != nil {
		s.actionError(w, r, "/customers", err)
		return
	}
	msg := "Cliente desativado."
	if active {
		msg = "Cliente ativado."
	}
	s.success(w, r, "/customers", msg)
}

// ---- payment types ----

func (s *Server) paymentTypeList(w http.ResponseWriter, r *http.Request) {
	items, err := s.svc.Payments.List(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	p := s.newPage(r, "Formas de pagamento", "payment-types")
	p.Data["PaymentTypes"] = items
	s.view(w, r, "payment_type_list", p)
}

func (s *Server) paymentTypeFormPage(w http.ResponseWriter, r *http.Request, status int, pt *domain.PaymentType, errs map[string]string) {
	title := "Nova forma de pagamento"
	if pt.ID != 0 {
		title = "Editar forma de pagamento"
	}
	p := s.newPage(r, title, "payment-types")
	p.Errors = orEmpty(errs)
	p.Data["PaymentType"] = pt
	s.viewStatus(w, r, status, "payment_type_form", p)
}

func (s *Server) paymentTypeNew(w http.ResponseWriter, r *http.Request) {
	s.paymentTypeFormPage(w, r, http.StatusOK, &domain.PaymentType{}, nil)
}

func (s *Server) paymentTypeEdit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, domain.ErrNotFound)
		return
	}
	pt, err := s.svc.Payments.Get(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.paymentTypeFormPage(w, r, http.StatusOK, pt, nil)
}

func (s *Server) paymentTypeSave(w http.ResponseWriter, r *http.Request, id int, msg string) {
	f := newForm(r)
	pt := &domain.PaymentType{
		ID: id, Name: f.str("name"), Description: f.str("description"),
		SurchargePercent: f.percent("surcharge_percent", "Acréscimo", false),
	}
	err := f.err()
	if err == nil {
		err = s.svc.Payments.Save(r.Context(), pt)
	}
	if fields, ok := fieldErrors(err); ok {
		s.paymentTypeFormPage(w, r, http.StatusUnprocessableEntity, pt, fields)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.success(w, r, "/payment-types", msg)
}

func (s *Server) paymentTypeCreate(w http.ResponseWriter, r *http.Request) {
	s.paymentTypeSave(w, r, 0, "Forma de pagamento cadastrada.")
}

func (s *Server) paymentTypeUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, domain.ErrNotFound)
		return
	}
	s.paymentTypeSave(w, r, id, "Forma de pagamento atualizada.")
}

func (s *Server) paymentTypeDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, domain.ErrNotFound)
		return
	}
	if err := s.svc.Payments.Delete(r.Context(), id); err != nil {
		s.actionError(w, r, "/payment-types", err)
		return
	}
	s.success(w, r, "/payment-types", "Forma de pagamento excluída.")
}
