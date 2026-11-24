package web

import (
	"fmt"
	"net/http"
	"strconv"

	"patylook/internal/core/domain"
	"patylook/internal/core/port"
)

// orderRow is one product line of the order form.
type orderRow struct {
	ProductID int
	Quantity  string
	Discount  string
}

// orderDraft holds the submitted order so the form can be re-rendered on errors.
type orderDraft struct {
	CustomerID    int
	PaymentTypeID int
	Discount      string
	Notes         string
	Rows          []orderRow
}

func (s *Server) orderList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	status := domain.OrderStatus(r.URL.Query().Get("status"))
	page, err := s.svc.Orders.List(r.Context(), pageParam(r), status, q)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !status.Valid() {
		status = ""
	}
	p := s.newPage(r, "Pedidos", "orders")
	p.Data["Orders"] = page.Items
	p.Data["Query"] = q
	p.Data["Status"] = string(status)
	p.Data["Pager"] = pagerFor(r, page)
	s.view(w, r, "order_list", p)
}

func (s *Server) orderFormPage(w http.ResponseWriter, r *http.Request, status int, draft orderDraft, errs map[string]string) {
	data, err := s.svc.Orders.FormData(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if len(draft.Rows) == 0 {
		draft.Rows = []orderRow{{Quantity: "1"}}
	}
	p := s.newPage(r, "Novo pedido", "orders")
	p.Errors = orEmpty(errs)
	p.Data["Form"] = draft
	p.Data["Customers"] = data.Customers
	p.Data["PaymentTypes"] = data.PaymentTypes
	p.Data["Products"] = data.Products
	s.viewStatus(w, r, status, "order_form", p)
}

func (s *Server) orderNew(w http.ResponseWriter, r *http.Request) {
	s.orderFormPage(w, r, http.StatusOK, orderDraft{}, nil)
}

func (s *Server) orderCreate(w http.ResponseWriter, r *http.Request) {
	f := newForm(r)
	draft := orderDraft{
		CustomerID:    f.integer("customer_id"),
		PaymentTypeID: f.integer("payment_type_id"),
		Discount:      f.str("discount"),
		Notes:         f.str("notes"),
	}
	in := port.CreateOrderInput{
		CustomerID:    draft.CustomerID,
		PaymentTypeID: draft.PaymentTypeID,
		Discount:      f.money("discount", "Desconto", false),
		Notes:         draft.Notes,
	}

	products, qtys, discounts := r.PostForm["product_id"], r.PostForm["quantity"], r.PostForm["item_discount"]
	for i, rawProduct := range products {
		if rawProduct == "" {
			continue // empty row
		}
		row := orderRow{Quantity: at(qtys, i), Discount: at(discounts, i)}
		row.ProductID, _ = strconv.Atoi(rawProduct)

		qty, err := strconv.Atoi(row.Quantity)
		if err != nil {
			f.errs.Add("items", "Quantidade inválida")
		}
		var disc domain.Money
		if row.Discount != "" {
			d, err := domain.ParseMoney(row.Discount)
			if err != nil {
				f.errs.Add("items", "Desconto do item inválido")
			}
			disc = d
		}
		draft.Rows = append(draft.Rows, row)
		in.Items = append(in.Items, port.OrderItemInput{ProductID: row.ProductID, Quantity: qty, Discount: disc})
	}

	err := f.err()
	var order *domain.Order
	if err == nil {
		order, err = s.svc.Orders.Create(r.Context(), principalFrom(r.Context()).User, in)
	}
	if fields, ok := fieldErrors(err); ok {
		s.orderFormPage(w, r, http.StatusUnprocessableEntity, draft, fields)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.success(w, r, fmt.Sprintf("/orders/%d", order.ID), fmt.Sprintf("Pedido #%d criado.", order.ID))
}

func at(list []string, i int) string {
	if i < len(list) {
		return list[i]
	}
	return ""
}

func (s *Server) orderShow(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, domain.ErrNotFound)
		return
	}
	order, err := s.svc.Orders.Get(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	p := s.newPage(r, fmt.Sprintf("Pedido #%d", order.ID), "orders")
	p.Data["Order"] = order
	s.view(w, r, "order_detail", p)
}

func (s *Server) orderChangeStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, domain.ErrNotFound)
		return
	}
	next := domain.OrderStatus(r.PostFormValue("status"))
	dest := fmt.Sprintf("/orders/%d", id)
	if next != domain.OrderCompleted && next != domain.OrderCanceled {
		s.redirect(w, r, dest, domain.FlashError, "Status inválido.")
		return
	}
	if err := s.svc.Orders.ChangeStatus(r.Context(), id, next); err != nil {
		s.actionError(w, r, dest, err)
		return
	}
	msg := "Pedido concluído."
	if next == domain.OrderCanceled {
		msg = "Pedido cancelado e estoque devolvido."
	}
	s.success(w, r, dest, msg)
}
