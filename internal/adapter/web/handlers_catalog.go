package web

import (
	"fmt"
	"net/http"
	"time"

	"patylook/internal/core/domain"
)

func (s *Server) productList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	page, err := s.svc.Catalog.ListProducts(r.Context(), pageParam(r), q)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	p := s.newPage(r, "Produtos", "products")
	p.Data["Products"] = page.Items
	p.Data["Query"] = q
	p.Data["Pager"] = pagerFor(r, page)
	s.view(w, r, "product_list", p)
}

func (s *Server) productFormPage(w http.ResponseWriter, r *http.Request, status int, product *domain.Product, errs map[string]string) {
	cats, err := s.svc.Catalog.ListCategories(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	brands, err := s.svc.Brands.ListActive(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	// A product keeps showing its brand after the brand is deactivated.
	if product.BrandID != 0 && product.BrandName != "" {
		listed := false
		for _, b := range brands {
			listed = listed || b.ID == product.BrandID
		}
		if !listed {
			brands = append(brands, domain.Brand{ID: product.BrandID, Name: product.BrandName})
		}
	}
	title := "Novo produto"
	var images []domain.ProductImage
	if product.ID != 0 {
		title = "Editar produto"
		if images, err = s.svc.Catalog.ListImages(r.Context(), product.ID); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	p := s.newPage(r, title, "products")
	p.Errors = orEmpty(errs)
	p.Data["Product"] = product
	p.Data["Categories"] = cats
	p.Data["Brands"] = brands
	p.Data["Images"] = images
	p.Data["HasMain"] = false
	for _, img := range images {
		if img.IsMain() {
			p.Data["HasMain"] = true
		}
	}
	p.Data["MaxExtra"] = domain.MaxExtraImages
	p.Data["MaxMB"] = domain.MaxImageBytes >> 20
	s.viewStatus(w, r, status, "product_form", p)
}

func (s *Server) productNew(w http.ResponseWriter, r *http.Request) {
	s.productFormPage(w, r, http.StatusOK, &domain.Product{IsActive: true}, nil)
}

func (s *Server) productEdit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, domain.ErrNotFound)
		return
	}
	s.productEditPage(w, r, http.StatusOK, id, nil)
}

// productEditPage renders the edit form (with the image manager) of a stored product.
func (s *Server) productEditPage(w http.ResponseWriter, r *http.Request, status, id int, errs map[string]string) {
	product, err := s.svc.Catalog.GetProduct(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.productFormPage(w, r, status, product, errs)
}

// productFromForm reads the editable fields. The SKU is not among them: the service generates it
// when the product is created and keeps it afterwards.
func productFromForm(f *form) *domain.Product {
	return &domain.Product{
		Name:        f.str("name"),
		Description: f.str("description"),
		CategoryID:  f.integer("product_category_id"),
		BrandID:     f.integer("brand_id"),
		Stock:       f.intField("stock", "Estoque"),
	}
}

func (s *Server) productCreate(w http.ResponseWriter, r *http.Request) {
	f := newForm(r)
	product := productFromForm(f)
	imgs, uploadErr := readUploads(r)
	if fields, ok := fieldErrors(uploadErr); ok {
		for k, m := range fields {
			f.errs.Add(k, m)
		}
	}
	err := f.err()
	if err == nil {
		err = s.svc.Catalog.CreateProduct(r.Context(), principalFrom(r.Context()).User, product, imgs)
	}
	if fields, ok := fieldErrors(err); ok {
		s.productFormPage(w, r, http.StatusUnprocessableEntity, product, fields)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.success(w, r, fmt.Sprintf("/products/%d/prices", product.ID), "Produto cadastrado. Defina agora o preço de venda.")
}

func (s *Server) productUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, domain.ErrNotFound)
		return
	}
	f := newForm(r)
	product := productFromForm(f)
	product.ID = id
	err := f.err()
	if err == nil {
		err = s.svc.Catalog.UpdateProduct(r.Context(), product)
	}
	if fields, ok := fieldErrors(err); ok {
		s.productFormPage(w, r, http.StatusUnprocessableEntity, product, fields)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.success(w, r, "/products", "Produto atualizado.")
}

func (s *Server) productSetActive(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, domain.ErrNotFound)
		return
	}
	active := r.PostFormValue("active") == "1"
	if err := s.svc.Catalog.SetProductActive(r.Context(), id, active); err != nil {
		s.actionError(w, r, "/products", err)
		return
	}
	msg := "Produto desativado."
	if active {
		msg = "Produto ativado."
	}
	s.success(w, r, "/products", msg)
}

// ---- pricebook ----

func (s *Server) priceFormPage(w http.ResponseWriter, r *http.Request, status int, productID int, entry *domain.Pricebook, errs map[string]string) {
	product, err := s.svc.Catalog.GetProduct(r.Context(), productID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	prices, err := s.svc.Catalog.ListPrices(r.Context(), productID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	p := s.newPage(r, "Preços — "+product.Name, "products")
	p.Errors = orEmpty(errs)
	p.Data["Product"] = product
	p.Data["Prices"] = prices
	p.Data["Form"] = entry
	s.viewStatus(w, r, status, "product_prices", p)
}

func (s *Server) priceList(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, domain.ErrNotFound)
		return
	}
	s.priceFormPage(w, r, http.StatusOK, id, &domain.Pricebook{ValidFrom: s.today()}, nil)
}

func (s *Server) priceCreate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, domain.ErrNotFound)
		return
	}
	f := newForm(r)
	entry := &domain.Pricebook{
		ProductID:     id,
		ProductCost:   f.money("product_cost", "Custo do produto", true),
		PackagingCost: f.money("packaging_cost", "Custo de embalagem", false),
		LogisticsCost: f.money("logistics_cost", "Custo logístico", false),
		OtherCosts:    f.money("other_costs", "Outros custos", false),
		FinalPrice:    f.money("final_price", "Preço final", true),
	}
	if from := f.date("valid_from", "Início da vigência", s.opts.Location); from != nil {
		entry.ValidFrom = *from
	}
	entry.ValidTo = f.date("valid_to", "Fim da vigência", s.opts.Location)

	err := f.err()
	if err == nil {
		err = s.svc.Catalog.AddPrice(r.Context(), principalFrom(r.Context()).User, entry)
	}
	if fields, ok := fieldErrors(err); ok {
		s.priceFormPage(w, r, http.StatusUnprocessableEntity, id, entry, fields)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.success(w, r, fmt.Sprintf("/products/%d/prices", id), "Preço cadastrado.")
}

// ---- product categories ----

func (s *Server) categoryList(w http.ResponseWriter, r *http.Request) {
	cats, err := s.svc.Catalog.ListCategories(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	p := s.newPage(r, "Categorias de produtos", "categories")
	p.Data["Categories"] = cats
	s.view(w, r, "category_list", p)
}

func (s *Server) categoryFormPage(w http.ResponseWriter, r *http.Request, status int, c *domain.ProductCategory, errs map[string]string) {
	title := "Nova categoria"
	if c.ID != 0 {
		title = "Editar categoria"
	}
	p := s.newPage(r, title, "categories")
	p.Errors = orEmpty(errs)
	p.Data["Category"] = c
	s.viewStatus(w, r, status, "category_form", p)
}

func (s *Server) categoryNew(w http.ResponseWriter, r *http.Request) {
	s.categoryFormPage(w, r, http.StatusOK, &domain.ProductCategory{}, nil)
}

func (s *Server) categoryEdit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, domain.ErrNotFound)
		return
	}
	c, err := s.svc.Catalog.GetCategory(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.categoryFormPage(w, r, http.StatusOK, c, nil)
}

func (s *Server) categorySave(w http.ResponseWriter, r *http.Request, id int, msg string) {
	f := newForm(r)
	c := &domain.ProductCategory{ID: id, Name: f.str("name"), Description: f.str("description")}
	err := s.svc.Catalog.SaveCategory(r.Context(), c)
	if fields, ok := fieldErrors(err); ok {
		s.categoryFormPage(w, r, http.StatusUnprocessableEntity, c, fields)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.success(w, r, "/categories", msg)
}

func (s *Server) categoryCreate(w http.ResponseWriter, r *http.Request) {
	s.categorySave(w, r, 0, "Categoria cadastrada.")
}

func (s *Server) categoryUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, domain.ErrNotFound)
		return
	}
	s.categorySave(w, r, id, "Categoria atualizada.")
}

func (s *Server) categoryDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, domain.ErrNotFound)
		return
	}
	if err := s.svc.Catalog.DeleteCategory(r.Context(), id); err != nil {
		s.actionError(w, r, "/categories", err)
		return
	}
	s.success(w, r, "/categories", "Categoria excluída.")
}

// today returns the current date in the application's time zone.
func (s *Server) today() time.Time {
	y, m, d := time.Now().In(s.opts.Location).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, s.opts.Location)
}

func orEmpty(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}
