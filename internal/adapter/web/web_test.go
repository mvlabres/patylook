package web_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"patylook/internal/adapter/memory"
	"patylook/internal/adapter/web"
	"patylook/internal/core/domain"
	"patylook/internal/core/port"
	"patylook/internal/core/service"
	assets "patylook/web"
)

var loc = time.FixedZone("BRT", -3*3600)

// ---- fakes ----

type plainHasher struct{}

func (plainHasher) Hash(p string) (string, error) { return "hash:" + p, nil }
func (plainHasher) Verify(h, p string) bool       { return h == "hash:"+p }

type userRepo struct {
	port.UserRepository
	users map[string]*domain.User
}

func (r userRepo) FindByUsername(_ context.Context, name string) (*domain.User, error) {
	if u, ok := r.users[name]; ok {
		c := *u
		return &c, nil
	}
	return nil, domain.ErrNotFound
}

func (r userRepo) FindByID(_ context.Context, id int) (*domain.User, error) {
	for _, u := range r.users {
		if u.ID == id {
			c := *u
			return &c, nil
		}
	}
	return nil, domain.ErrNotFound
}

// Stubs embed the port so any call the test did not anticipate panics.

var (
	sampleImages = []domain.ProductImage{
		{ID: 1, ProductID: 1, FileName: "principal.jpg", Path: "images/products/CAM-1/principal.jpg"},
		{ID: 2, ProductID: 1, FileName: "imagem-01.jpg", Path: "images/products/CAM-1/imagem-01.jpg", SortOrder: 1},
	}
	sampleCategory = domain.ProductCategory{ID: 1, Name: "Roupas", Description: "Vestuário"}
	samplePrice    = domain.Money(5250)
	sampleProduct  = domain.Product{ID: 1, Name: "Camiseta", SKU: "CAM-1", CategoryID: 1, CategoryName: "Roupas", Stock: 3, IsActive: true, CurrentPrice: &samplePrice}
	sampleCustomer = domain.Customer{ID: 1, Name: "Maria", Document: "52998224725", Phone: "11999999999", Email: "m@x.com", IsActive: true}
	sampleBrand    = domain.Brand{ID: 1, Name: "Natura", IsActive: true, CreatedBy: 1, CreatedByName: "Ana", CreatedAt: time.Now()}
	samplePayment  = domain.PaymentType{ID: 1, Name: "Cartão", Description: "Crédito", SurchargePercent: 500}
	sampleOrder    = domain.Order{
		ID: 7, Status: domain.OrderOpen, CustomerName: "Maria", PaymentTypeName: "Cartão", UserName: "Ana",
		Subtotal: 10000, Total: 10500, PaymentAddition: 500, Notes: "entregar <b>cedo</b>", CreatedAt: time.Now(),
		Items: []domain.OrderItem{{ProductID: 1, ProductName: "Camiseta", ProductSKU: "CAM-1", Quantity: 2, UnitPrice: 5000}},
	}
)

func invalid(field, msg string) error {
	v := domain.NewValidationError()
	v.Add(field, msg)
	return v
}

type catalogStub struct{ port.CatalogService }

func (catalogStub) ListCategories(context.Context) ([]domain.ProductCategory, error) {
	return []domain.ProductCategory{sampleCategory}, nil
}
func (catalogStub) GetCategory(context.Context, int) (*domain.ProductCategory, error) {
	c := sampleCategory
	return &c, nil
}
func (catalogStub) SaveCategory(context.Context, *domain.ProductCategory) error {
	return invalid("name", "Nome é obrigatório")
}
func (catalogStub) ListProducts(context.Context, int, string) (port.Page[domain.Product], error) {
	return port.Page[domain.Product]{Items: []domain.Product{sampleProduct}, Total: 41, Page: 1, PageSize: 20}, nil
}
func (catalogStub) GetProduct(context.Context, int) (*domain.Product, error) {
	p := sampleProduct
	return &p, nil
}

// lastUpload records what the product create/image handlers passed to the service.
var lastUpload port.ProductImages

func (catalogStub) CreateProduct(_ context.Context, _ *domain.User, _ *domain.Product, imgs port.ProductImages) error {
	lastUpload = imgs
	return invalid("sku", "Já existe um produto com esse SKU")
}
func (catalogStub) ListImages(context.Context, int) ([]domain.ProductImage, error) {
	return sampleImages, nil
}
func (catalogStub) AddImages(_ context.Context, _ *domain.User, _ int, imgs port.ProductImages) error {
	lastUpload = imgs
	if imgs.Main == nil && len(imgs.Extra) == 0 {
		return invalid("images", "Selecione ao menos uma imagem")
	}
	return nil
}
func (catalogStub) DeleteImage(_ context.Context, _, imageID int) error {
	if imageID == 1 {
		return invalid("", "A imagem principal não pode ser removida")
	}
	return nil
}
func (catalogStub) Showcase(context.Context, int, string) (port.Page[port.ShowcaseItem], error) {
	return port.Page[port.ShowcaseItem]{
		Items: []port.ShowcaseItem{
			{Product: sampleProduct, Images: sampleImages},
			{Product: domain.Product{ID: 2, Name: "Sem foto", SKU: "SF-1", CategoryName: "Roupas", IsActive: true}},
		},
		Total: 2, Page: 1, PageSize: 20,
	}, nil
}

type fileHandle struct{ *bytes.Reader }

func (fileHandle) Close() error { return nil }

func (catalogStub) OpenImage(_ context.Context, folder, name string) (io.ReadSeekCloser, time.Time, error) {
	if folder == "CAM-1" && name == "principal.jpg" {
		return fileHandle{bytes.NewReader([]byte("fake-jpeg-bytes"))}, time.Unix(1700000000, 0), nil
	}
	return nil, time.Time{}, domain.ErrNotFound
}
func (catalogStub) ListPrices(context.Context, int) ([]domain.Pricebook, error) {
	return []domain.Pricebook{{ID: 1, ValidFrom: time.Now(), FinalPrice: 5250, CreatedByName: "Ana", CreatedAt: time.Now()}}, nil
}
func (catalogStub) AddPrice(context.Context, *domain.User, *domain.Pricebook) error { return nil }

type customerStub struct{ port.CustomerService }

func (customerStub) List(context.Context, int, string) (port.Page[domain.Customer], error) {
	return port.Page[domain.Customer]{Items: []domain.Customer{sampleCustomer}, Total: 1, Page: 1, PageSize: 20}, nil
}
func (customerStub) Get(context.Context, int) (*domain.Customer, error) {
	c := sampleCustomer
	return &c, nil
}
func (customerStub) Save(context.Context, *domain.Customer) error {
	return invalid("document", "CPF inválido")
}

type brandStub struct{ port.BrandService }

func (brandStub) List(context.Context, int, string) (port.Page[domain.Brand], error) {
	return port.Page[domain.Brand]{Items: []domain.Brand{sampleBrand}, Total: 1, Page: 1, PageSize: 20}, nil
}
func (brandStub) ListActive(context.Context) ([]domain.Brand, error) {
	return []domain.Brand{sampleBrand}, nil
}
func (brandStub) Get(context.Context, int) (*domain.Brand, error) {
	b := sampleBrand
	return &b, nil
}
func (brandStub) Save(context.Context, *domain.User, *domain.Brand) error {
	return invalid("name", "Já existe uma marca com esse nome")
}

type paymentStub struct{ port.PaymentTypeService }

func (paymentStub) List(context.Context) ([]domain.PaymentType, error) {
	return []domain.PaymentType{samplePayment}, nil
}
func (paymentStub) Get(context.Context, int) (*domain.PaymentType, error) {
	p := samplePayment
	return &p, nil
}
func (paymentStub) Delete(context.Context, int) error { return domain.ErrInUse }

type orderStub struct{ port.OrderService }

func (orderStub) List(context.Context, int, domain.OrderStatus, string) (port.Page[domain.Order], error) {
	return port.Page[domain.Order]{Items: []domain.Order{sampleOrder}, Total: 1, Page: 1, PageSize: 20}, nil
}
func (orderStub) Get(context.Context, int) (*domain.Order, error) {
	o := sampleOrder
	return &o, nil
}
func (orderStub) FormData(context.Context) (*port.OrderFormData, error) {
	return &port.OrderFormData{
		Customers: []domain.Customer{sampleCustomer}, PaymentTypes: []domain.PaymentType{samplePayment},
		Products: []domain.Product{sampleProduct},
	}, nil
}
func (orderStub) Create(context.Context, *domain.User, port.CreateOrderInput) (*domain.Order, error) {
	return nil, invalid("items", "Estoque insuficiente para Camiseta")
}
func (orderStub) ChangeStatus(context.Context, int, domain.OrderStatus) error {
	return domain.ErrInvalidTransition
}

type dashboardStub struct{}

func (dashboardStub) Load(context.Context) (*port.Dashboard, error) {
	return &port.Dashboard{
		Summary:      port.OrderSummary{OpenOrders: 2, CompletedOrders: 5, CompletedTotal: 123456},
		LowStock:     1,
		RecentOrders: []domain.Order{sampleOrder},
		PeriodStart:  time.Now(),
	}, nil
}

type userStub struct{ port.UserService }

func (userStub) List(context.Context, *domain.User, int, string) (port.Page[domain.User], error) {
	return port.Page[domain.User]{Items: []domain.User{{ID: 2, Username: "joao", Name: "João", CategoryName: "operator", IsActive: true}}, Total: 1, Page: 1, PageSize: 20}, nil
}
func (userStub) Get(context.Context, *domain.User, int) (*domain.User, error) {
	return &domain.User{ID: 2, Username: "joao", Name: "João", CategoryID: 2, IsActive: true}, nil
}
func (userStub) Categories(context.Context) ([]domain.UserCategory, error) {
	return []domain.UserCategory{{ID: 1, Name: "admin"}, {ID: 2, Name: "operator"}}, nil
}

// ---- harness ----

type app struct {
	t       *testing.T
	handler http.Handler
}

func newApp(t *testing.T) *app {
	t.Helper()
	repo := userRepo{users: map[string]*domain.User{
		"ana":  {ID: 1, Username: "ana", Name: "Ana", PasswordHash: "hash:secret123", IsActive: true, CategoryName: "admin"},
		"joao": {ID: 2, Username: "joao", Name: "João", PasswordHash: "hash:secret123", IsActive: true, CategoryName: "operator"},
	}}
	clock := func() time.Time { return time.Now().In(loc) }
	auth, err := service.NewAuthService(repo, memory.NewSessionStore(), plainHasher{},
		service.AuthConfig{SessionTTL: time.Hour, AdminCategory: "admin"}, clock)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := web.NewServer(web.Services{
		Auth: auth, Users: userStub{}, Catalog: catalogStub{}, Brands: brandStub{}, Customers: customerStub{},
		Payments: paymentStub{}, Orders: orderStub{}, Dashboard: dashboardStub{},
	}, web.Options{Assets: assets.Files, Location: loc, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	h, err := srv.Handler()
	if err != nil {
		t.Fatal(err)
	}
	return &app{t: t, handler: h}
}

type response struct {
	*httptest.ResponseRecorder
	body string
}

func (a *app) do(method, path string, form url.Values, cookie *http.Cookie, headers map[string]string) response {
	a.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, path, body)
	req.RemoteAddr = "203.0.113.5:1234"
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	b, _ := io.ReadAll(rec.Body)
	return response{rec, string(b)}
}

func (a *app) login(user string) *http.Cookie {
	a.t.Helper()
	res := a.do("POST", "/login", url.Values{"username": {user}, "password": {"secret123"}}, nil, nil)
	if res.Code != http.StatusSeeOther {
		a.t.Fatalf("login %s: status %d\n%s", user, res.Code, res.body)
	}
	for _, c := range res.Result().Cookies() {
		if c.Name == "patylook_session" {
			return c
		}
	}
	a.t.Fatal("no session cookie")
	return nil
}

// csrf scrapes the CSRF token from a rendered page.
func (a *app) csrf(cookie *http.Cookie) string {
	a.t.Helper()
	res := a.do("GET", "/profile", nil, cookie, nil)
	const marker = `name="csrf_token" value="`
	i := strings.Index(res.body, marker)
	if i < 0 {
		a.t.Fatalf("csrf token not found in page:\n%s", res.body)
	}
	rest := res.body[i+len(marker):]
	return rest[:strings.IndexByte(rest, '"')]
}

// ---- tests ----

func TestAuthenticationFlow(t *testing.T) {
	a := newApp(t)

	res := a.do("GET", "/", nil, nil, nil)
	if res.Code != http.StatusSeeOther || res.Header().Get("Location") != "/login" {
		t.Errorf("anonymous GET /: %d %s", res.Code, res.Header().Get("Location"))
	}
	res = a.do("GET", "/orders", nil, &http.Cookie{Name: "patylook_session", Value: "forged"}, nil)
	if res.Code != http.StatusSeeOther {
		t.Errorf("forged cookie: %d", res.Code)
	}

	res = a.do("GET", "/login", nil, nil, nil)
	if res.Code != 200 || !strings.Contains(res.body, `name="password"`) {
		t.Errorf("login page: %d", res.Code)
	}

	res = a.do("POST", "/login", url.Values{"username": {"ana"}, "password": {"wrong"}}, nil, nil)
	if res.Code != http.StatusUnauthorized || !strings.Contains(res.body, "inválidos") {
		t.Errorf("bad login: %d", res.Code)
	}

	cookie := a.login("ana")
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("cookie flags: %+v", cookie)
	}
	res = a.do("GET", "/", nil, cookie, nil)
	if res.Code != 200 || !strings.Contains(res.body, "Painel") || !strings.Contains(res.body, "R$ 1.234,56") {
		t.Errorf("dashboard: %d", res.Code)
	}
	if res = a.do("GET", "/login", nil, cookie, nil); res.Code != http.StatusSeeOther {
		t.Errorf("logged user on /login: %d", res.Code)
	}

	// Logout requires the CSRF token, then kills the session.
	if res = a.do("POST", "/logout", url.Values{}, cookie, nil); res.Code != http.StatusForbidden {
		t.Errorf("logout without csrf: %d", res.Code)
	}
	token := a.csrf(cookie)
	if res = a.do("POST", "/logout", url.Values{"csrf_token": {token}}, cookie, nil); res.Code != http.StatusSeeOther {
		t.Errorf("logout: %d", res.Code)
	}
	if res = a.do("GET", "/", nil, cookie, nil); res.Code != http.StatusSeeOther {
		t.Errorf("session survived logout: %d", res.Code)
	}
}

func TestCrossOriginPostsAreRejected(t *testing.T) {
	a := newApp(t)
	form := url.Values{"username": {"ana"}, "password": {"secret123"}}

	for name, headers := range map[string]map[string]string{
		"sec-fetch-site":  {"Sec-Fetch-Site": "cross-site"},
		"foreign origin":  {"Origin": "https://evil.example"},
		"same-site (sub)": {"Sec-Fetch-Site": "same-site"},
	} {
		if res := a.do("POST", "/login", form, nil, headers); res.Code != http.StatusForbidden {
			t.Errorf("%s: %d", name, res.Code)
		}
	}
	headers := map[string]string{"Origin": "http://example.com", "Sec-Fetch-Site": "same-origin"}
	if res := a.do("POST", "/login", form, nil, headers); res.Code != http.StatusSeeOther {
		t.Errorf("same-origin login: %d", res.Code)
	}
}

func TestAdminOnlyRoutes(t *testing.T) {
	a := newApp(t)
	operator := a.login("joao")
	for _, path := range []string{"/users", "/users/new", "/users/2/edit"} {
		if res := a.do("GET", path, nil, operator, nil); res.Code != http.StatusForbidden {
			t.Errorf("operator GET %s: %d", path, res.Code)
		}
	}
	res := a.do("GET", "/orders", nil, operator, nil)
	if res.Code != 200 || strings.Contains(res.body, `href="/users"`) {
		t.Errorf("operator nav/orders: %d", res.Code)
	}

	admin := a.login("ana")
	for _, path := range []string{"/users", "/users/new", "/users/2/edit"} {
		if res := a.do("GET", path, nil, admin, nil); res.Code != 200 {
			t.Errorf("admin GET %s: %d", path, res.Code)
		}
	}
}

func TestPagesRender(t *testing.T) {
	a := newApp(t)
	admin := a.login("ana")

	pages := map[string]string{
		"/":                                    "Pedidos recentes",
		"/products":                            "Camiseta",
		"/products?page=1&q=cam":               "Próxima",
		"/products/new":                        "images/products/SKU/",
		"/showcase":                            `data-src="/images/products/CAM-1/imagem-01.jpg"`,
		"/images/products/CAM-1/principal.jpg": "fake-jpeg-bytes",
		"/products/1/edit":                     "Remover",
		"/products/1/prices":                   "Nova vigência de preço",
		"/categories":                          "Roupas",
		"/categories/new":                      "Nova categoria",
		"/categories/1/edit":                   "Editar categoria",
		"/brands":                              "Natura",
		"/brands/new":                          "Nova marca",
		"/brands/1/edit":                       "Editar marca",
		"/customers":                           "529.982.247-25",
		"/customers/new":                       "Novo cliente",
		"/customers/1/edit":                    "Editar cliente",
		"/payment-types":                       "Cartão",
		"/payment-types/new":                   "Nova forma de pagamento",
		"/payment-types/1/edit":                "Editar forma de pagamento",
		"/orders":                              "#7",
		"/orders?status=open":                  "Aberto",
		"/orders/new":                          "Adicionar item",
		"/orders/7":                            "Pedido #7",
		"/users":                               "joao",
		"/users/new":                           "Novo usuário",
		"/users/2/edit":                        "Redefinir senha",
		"/profile":                             "Alterar senha",
		"/healthz":                             "ok",
		"/static/css/app.css":                  "--primary",
		"/static/js/order.js":                  "recalc",
	}
	for path, want := range pages {
		res := a.do("GET", path, nil, admin, nil)
		if res.Code != 200 || !strings.Contains(res.body, want) {
			t.Errorf("GET %s: status %d, missing %q\n%.300s", path, res.Code, want, res.body)
		}
	}

	// User content must be escaped.
	res := a.do("GET", "/orders/7", nil, admin, nil)
	if strings.Contains(res.body, "<b>cedo</b>") || !strings.Contains(res.body, "&lt;b&gt;cedo&lt;/b&gt;") {
		t.Error("order notes are not HTML-escaped")
	}

	for path, want := range map[string]int{
		"/nope": 404, "/products/abc/edit": 404, "/static/": 404, "/static/css/": 404,
	} {
		if res := a.do("GET", path, nil, admin, nil); res.Code != want {
			t.Errorf("GET %s: %d, want %d", path, res.Code, want)
		}
	}
}

func TestProductFormFieldsAndReadOnlySKU(t *testing.T) {
	a := newApp(t)
	admin := a.login("ana")

	res := a.do("GET", "/products/new", nil, admin, nil)
	if res.Code != 200 {
		t.Fatalf("GET /products/new: %d", res.Code)
	}
	// Order: nome, categoria, marca, SKU, estoque, descrição.
	last := -1
	for _, id := range []string{`id="f-name"`, `id="f-product_category_id"`, `id="f-brand_id"`, `id="f-sku"`, `id="f-stock"`, `id="f-description"`} {
		i := strings.Index(res.body, id)
		if i < 0 || i < last {
			t.Fatalf("field %s missing or out of order (at %d, previous at %d)", id, i, last)
		}
		last = i
	}
	if !strings.Contains(res.body, "Natura") {
		t.Error("the brand select must list the active brands")
	}
	if !strings.Contains(res.body, "data-sku-preview") || !strings.Contains(res.body, "/static/js/product_form.js") {
		t.Error("new product form must preview the SKU")
	}
	// The SKU is read-only and has no name, so it is never submitted.
	if !strings.Contains(res.body, `id="f-sku" type="text"`) || strings.Contains(res.body, `name="sku"`) {
		t.Error("the SKU input must be read-only and unnamed")
	}
	if !strings.Contains(res.body, "readonly") {
		t.Error("SKU input must be readonly")
	}

	// The edit form shows the stored SKU, also read-only, without the preview script behavior.
	res = a.do("GET", "/products/1/edit", nil, admin, nil)
	if res.Code != 200 || !strings.Contains(res.body, `value="CAM-1" readonly`) {
		t.Errorf("edit form must show the SKU read-only: %d\n%.600s", res.Code, res.body)
	}
	if strings.Contains(res.body, "data-sku-preview") || strings.Contains(res.body, `name="sku"`) {
		t.Error("edit form must not preview or submit the SKU")
	}
}

func TestPriceFormInformsFinalPriceAndShowsReadOnlyMarkup(t *testing.T) {
	a := newApp(t)
	admin := a.login("ana")

	res := a.do("GET", "/products/1/prices", nil, admin, nil)
	if res.Code != 200 {
		t.Fatalf("GET /products/1/prices: %d", res.Code)
	}
	// The final price is typed (and submitted); the markup is shown, read-only and never submitted.
	if !strings.Contains(res.body, `name="final_price"`) {
		t.Error("final price must be an input of the form")
	}
	if strings.Contains(res.body, `name="markup_percent"`) {
		t.Error("markup must not be submitted")
	}
	if !strings.Contains(res.body, `id="f-markup_percent" type="text" value="0,00" readonly`) {
		t.Errorf("markup must be visible and read-only\n%.1500s", res.body)
	}
	if !strings.Contains(res.body, "/static/js/product_prices.js") {
		t.Error("page must load the live markup script")
	}
	if i, j := strings.Index(res.body, `id="f-final_price"`), strings.Index(res.body, `id="f-markup_percent"`); i < 0 || j < i {
		t.Error("final price must come before the markup")
	}
}

func TestFormErrorsRerenderWithStatus422(t *testing.T) {
	a := newApp(t)
	admin := a.login("ana")
	token := a.csrf(admin)

	cases := []struct {
		path string
		form url.Values
		want string
	}{
		{"/products", url.Values{"name": {"X"}, "sku": {"A"}, "product_category_id": {"1"}, "stock": {"1"}}, "Já existe um produto com esse SKU"},
		{"/products", url.Values{"name": {"X"}, "stock": {"abc"}}, "Estoque inválido"},
		{"/products/1/prices", url.Values{"product_cost": {"abc"}, "final_price": {"10"}, "valid_from": {"2026-01-01"}}, "Custo do produto inválido"},
		{"/products/1/prices", url.Values{"product_cost": {"10"}, "final_price": {"x"}, "valid_from": {"2026-01-01"}}, "Preço final inválido"},
		{"/categories", url.Values{"name": {""}}, "Nome é obrigatório"},
		{"/brands", url.Values{"name": {"Natura"}}, "Já existe uma marca com esse nome"},
		{"/customers", url.Values{"name": {"Z"}, "document": {"1"}}, "CPF inválido"},
		{"/orders", url.Values{"customer_id": {"1"}, "payment_type_id": {"1"}, "product_id": {"1"}, "quantity": {"2"}, "item_discount": {""}}, "Estoque insuficiente"},
		{"/orders", url.Values{"customer_id": {"1"}, "payment_type_id": {"1"}, "product_id": {"1"}, "quantity": {"x"}}, "Quantidade inválida"},
		{"/profile/password", url.Values{"current_password": {"a"}, "new_password": {"b"}, "confirm_password": {"c"}}, "A confirmação não confere"},
	}
	for _, c := range cases {
		c.form.Set("csrf_token", token)
		res := a.do("POST", c.path, c.form, admin, nil)
		if res.Code != http.StatusUnprocessableEntity || !strings.Contains(res.body, c.want) {
			t.Errorf("POST %s: status %d, missing %q\n%.400s", c.path, res.Code, c.want, res.body)
		}
	}

	// Form submissions without a valid token are rejected before reaching any service.
	res := a.do("POST", "/products", url.Values{"name": {"X"}}, admin, nil)
	if res.Code != http.StatusForbidden {
		t.Errorf("POST without csrf: %d", res.Code)
	}
}

func TestActionErrorsBecomeFlashMessages(t *testing.T) {
	a := newApp(t)
	admin := a.login("ana")
	token := a.csrf(admin)

	// Deleting a payment type that is in use redirects back with an error flash.
	res := a.do("POST", "/payment-types/1/delete", url.Values{"csrf_token": {token}}, admin, nil)
	if res.Code != http.StatusSeeOther || res.Header().Get("Location") != "/payment-types" {
		t.Fatalf("delete: %d %s", res.Code, res.Header().Get("Location"))
	}
	page := a.do("GET", "/payment-types", nil, admin, nil)
	if !strings.Contains(page.body, "alert-error") || !strings.Contains(page.body, "sendo usado") {
		t.Error("flash message not shown")
	}
	// The flash is shown only once.
	if again := a.do("GET", "/payment-types", nil, admin, nil); strings.Contains(again.body, "alert-error") {
		t.Error("flash shown twice")
	}

	res = a.do("POST", "/orders/7/status", url.Values{"csrf_token": {token}, "status": {"completed"}}, admin, nil)
	if res.Code != http.StatusSeeOther || res.Header().Get("Location") != "/orders/7" {
		t.Errorf("status change: %d", res.Code)
	}
	res = a.do("POST", "/orders/7/status", url.Values{"csrf_token": {token}, "status": {"open"}}, admin, nil)
	if res.Code != http.StatusSeeOther {
		t.Errorf("invalid status: %d", res.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	a := newApp(t)
	res := a.do("GET", "/login", nil, nil, nil)
	for _, h := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy"} {
		if res.Header().Get(h) == "" {
			t.Errorf("missing header %s", h)
		}
	}
}

// multipartRequest builds an authenticated multipart POST with form fields and files.
func (a *app) multipartPost(path string, fields map[string]string, files []testFile, cookie *http.Cookie) response {
	a.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	for _, f := range files {
		part, err := mw.CreateFormFile(f.field, f.name)
		if err != nil {
			a.t.Fatal(err)
		}
		_, _ = part.Write(f.data)
	}
	_ = mw.Close()

	req := httptest.NewRequest("POST", path, &body)
	req.RemoteAddr = "203.0.113.5:1234"
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	b, _ := io.ReadAll(rec.Body)
	return response{rec, string(b)}
}

type testFile struct {
	field, name string
	data        []byte
}

func TestProductUploadsAreParsed(t *testing.T) {
	a := newApp(t)
	admin := a.login("ana")
	token := a.csrf(admin)
	fields := map[string]string{"csrf_token": token, "name": "X", "sku": "A", "product_category_id": "1", "stock": "1"}

	lastUpload = port.ProductImages{}
	res := a.multipartPost("/products", fields, []testFile{
		{"main_image", "foto.png", []byte("main-bytes")},
		{"images", "a.jpg", []byte("extra-1")},
		{"images", "b.jpg", []byte("extra-2")},
	}, admin)
	// The stub rejects the SKU, which proves the request reached the service and re-rendered the form.
	if res.Code != http.StatusUnprocessableEntity || !strings.Contains(res.body, "Já existe um produto com esse SKU") {
		t.Fatalf("status %d\n%.300s", res.Code, res.body)
	}
	if lastUpload.Main == nil || lastUpload.Main.Filename != "foto.png" || string(lastUpload.Main.Data) != "main-bytes" {
		t.Errorf("main upload: %+v", lastUpload.Main)
	}
	if len(lastUpload.Extra) != 2 || string(lastUpload.Extra[1].Data) != "extra-2" {
		t.Errorf("extra uploads: %+v", lastUpload.Extra)
	}

	// Blank file inputs (empty filename) are ignored.
	lastUpload = port.ProductImages{}
	a.multipartPost("/products", fields, []testFile{{"main_image", "", nil}, {"images", "", nil}}, admin)
	if lastUpload.Main != nil || len(lastUpload.Extra) != 0 {
		t.Errorf("blank inputs should be ignored: %+v", lastUpload)
	}

	// Multipart posts still need a valid CSRF token.
	delete(fields, "csrf_token")
	if res := a.multipartPost("/products", fields, nil, admin); res.Code != http.StatusForbidden {
		t.Errorf("multipart without csrf: %d", res.Code)
	}
}

func TestImageManagement(t *testing.T) {
	a := newApp(t)
	admin := a.login("ana")
	token := a.csrf(admin)

	res := a.multipartPost("/products/1/images", map[string]string{"csrf_token": token}, nil, admin)
	if res.Code != http.StatusUnprocessableEntity || !strings.Contains(res.body, "Selecione ao menos uma imagem") {
		t.Errorf("empty upload: %d", res.Code)
	}
	res = a.multipartPost("/products/1/images", map[string]string{"csrf_token": token},
		[]testFile{{"images", "x.jpg", []byte("data")}}, admin)
	if res.Code != http.StatusSeeOther || res.Header().Get("Location") != "/products/1/edit" {
		t.Errorf("upload: %d %s", res.Code, res.Header().Get("Location"))
	}

	// Deleting the main image fails with a flash; a carousel image succeeds.
	res = a.do("POST", "/products/1/images/1/delete", url.Values{"csrf_token": {token}}, admin, nil)
	if res.Code != http.StatusSeeOther {
		t.Errorf("delete main: %d", res.Code)
	}
	if page := a.do("GET", "/products/1/edit", nil, admin, nil); !strings.Contains(page.body, "não pode ser removida") {
		t.Error("delete error flash not shown")
	}
	res = a.do("POST", "/products/1/images/2/delete", url.Values{"csrf_token": {token}}, admin, nil)
	if res.Code != http.StatusSeeOther {
		t.Errorf("delete extra: %d", res.Code)
	}
	if res := a.do("POST", "/products/1/images/abc/delete", url.Values{"csrf_token": {token}}, admin, nil); res.Code != http.StatusNotFound {
		t.Errorf("bad image id: %d", res.Code)
	}
}

func TestImageServing(t *testing.T) {
	a := newApp(t)
	admin := a.login("ana")

	res := a.do("GET", "/images/products/CAM-1/principal.jpg", nil, admin, nil)
	if res.Code != 200 || res.Header().Get("Content-Type") != "image/jpeg" || res.Header().Get("Last-Modified") == "" {
		t.Errorf("serve: %d %v", res.Code, res.Header())
	}
	if res := a.do("GET", "/images/products/CAM-1/missing.jpg", nil, admin, nil); res.Code != http.StatusNotFound {
		t.Errorf("missing: %d", res.Code)
	}
	// Images are only for signed-in users.
	if res := a.do("GET", "/images/products/CAM-1/principal.jpg", nil, nil, nil); res.Code != http.StatusSeeOther {
		t.Errorf("anonymous: %d", res.Code)
	}
}

func TestOversizedUploadIsRejected(t *testing.T) {
	a := newApp(t)
	admin := a.login("ana")
	req := httptest.NewRequest("POST", "/products", strings.NewReader("x"))
	req.ContentLength = 1 << 30
	req.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	req.AddCookie(admin)
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status %d", rec.Code)
	}
}
