package port

import (
	"context"
	"io"
	"time"

	"patylook/internal/core/domain"
)

// Inbound ports: the use cases the application exposes to driving adapters (HTTP).

type LoginResult struct {
	Token   string // raw cookie value
	Session domain.Session
	User    *domain.User
}

type AuthService interface {
	// Login authenticates and opens a session. clientKey identifies the caller for throttling.
	Login(ctx context.Context, clientKey, username, password string) (*LoginResult, error)
	// Authenticate resolves the raw cookie token into the active session and user.
	Authenticate(ctx context.Context, token string) (*domain.Session, *domain.User, error)
	Logout(ctx context.Context, token string) error
	SetFlash(ctx context.Context, s *domain.Session, kind domain.FlashKind, msg string) error
	// PopFlash returns and clears the pending flash message.
	PopFlash(ctx context.Context, s *domain.Session) *domain.Flash
}

type UserService interface {
	List(ctx context.Context, actor *domain.User, page int, search string) (Page[domain.User], error)
	Get(ctx context.Context, actor *domain.User, id int) (*domain.User, error)
	Categories(ctx context.Context) ([]domain.UserCategory, error)
	Create(ctx context.Context, actor *domain.User, u *domain.User, password string) error
	Update(ctx context.Context, actor *domain.User, u *domain.User) error
	ResetPassword(ctx context.Context, actor *domain.User, id int, password string) error
	SetActive(ctx context.Context, actor *domain.User, id int, active bool) error
	ChangeOwnPassword(ctx context.Context, actor *domain.User, current, next string) error
}

type CatalogService interface {
	ListCategories(ctx context.Context) ([]domain.ProductCategory, error)
	GetCategory(ctx context.Context, id int) (*domain.ProductCategory, error)
	SaveCategory(ctx context.Context, c *domain.ProductCategory) error // create when ID == 0
	DeleteCategory(ctx context.Context, id int) error

	ListProducts(ctx context.Context, page int, search string) (Page[domain.Product], error)
	GetProduct(ctx context.Context, id int) (*domain.Product, error)
	// CreateProduct registers the product together with its images; the main image is required.
	CreateProduct(ctx context.Context, actor *domain.User, p *domain.Product, imgs ProductImages) error
	UpdateProduct(ctx context.Context, p *domain.Product) error
	SetProductActive(ctx context.Context, id int, active bool) error

	ListPrices(ctx context.Context, productID int) ([]domain.Pricebook, error)
	AddPrice(ctx context.Context, actor *domain.User, p *domain.Pricebook) error

	ListImages(ctx context.Context, productID int) ([]domain.ProductImage, error)
	// AddImages replaces the main image (when given) and appends carousel images.
	AddImages(ctx context.Context, actor *domain.User, productID int, imgs ProductImages) error
	DeleteImage(ctx context.Context, productID, imageID int) error
	// Showcase lists active products with their images.
	Showcase(ctx context.Context, page int, search string) (Page[ShowcaseItem], error)
	// OpenImage opens a stored image file for serving.
	OpenImage(ctx context.Context, folder, name string) (io.ReadSeekCloser, time.Time, error)
}

// ImageUpload is a raw uploaded file.
type ImageUpload struct {
	Filename string
	Data     []byte
}

// ProductImages groups the uploads of one request: an optional main image and carousel images.
type ProductImages struct {
	Main  *ImageUpload
	Extra []ImageUpload
}

// ShowcaseItem is a product as shown in the showcase.
type ShowcaseItem struct {
	Product domain.Product
	Images  []domain.ProductImage // main first
}

// Main returns the main image, or nil when the product has none.
func (s ShowcaseItem) Main() *domain.ProductImage {
	for i := range s.Images {
		if s.Images[i].IsMain() {
			return &s.Images[i]
		}
	}
	return nil
}

type CustomerService interface {
	List(ctx context.Context, page int, search string) (Page[domain.Customer], error)
	Get(ctx context.Context, id int) (*domain.Customer, error)
	Save(ctx context.Context, c *domain.Customer) error // create when ID == 0
	SetActive(ctx context.Context, id int, active bool) error
}

type BrandService interface {
	List(ctx context.Context, page int, search string) (Page[domain.Brand], error)
	// ListActive returns every active brand, for selection lists.
	ListActive(ctx context.Context) ([]domain.Brand, error)
	Get(ctx context.Context, id int) (*domain.Brand, error)
	// Save creates the brand when ID == 0 (recording actor as its creator) or updates it.
	Save(ctx context.Context, actor *domain.User, b *domain.Brand) error
	SetActive(ctx context.Context, id int, active bool) error
}

type PaymentTypeService interface {
	List(ctx context.Context) ([]domain.PaymentType, error)
	Get(ctx context.Context, id int) (*domain.PaymentType, error)
	Save(ctx context.Context, p *domain.PaymentType) error // create when ID == 0
	Delete(ctx context.Context, id int) error
}

type OrderItemInput struct {
	ProductID int
	Quantity  int
	Discount  domain.Money
}

type CreateOrderInput struct {
	CustomerID    int
	PaymentTypeID int
	Discount      domain.Money
	Notes         string
	Items         []OrderItemInput
}

// OrderFormData holds the lookups needed to render the order form.
type OrderFormData struct {
	Customers    []domain.Customer
	PaymentTypes []domain.PaymentType
	Products     []domain.Product
}

type OrderService interface {
	FormData(ctx context.Context) (*OrderFormData, error)
	Create(ctx context.Context, actor *domain.User, in CreateOrderInput) (*domain.Order, error)
	Get(ctx context.Context, id int) (*domain.Order, error)
	List(ctx context.Context, page int, status domain.OrderStatus, search string) (Page[domain.Order], error)
	ChangeStatus(ctx context.Context, id int, next domain.OrderStatus) error
}

type Dashboard struct {
	Summary      OrderSummary
	LowStock     int
	RecentOrders []domain.Order
	PeriodStart  time.Time
}

type DashboardService interface {
	Load(ctx context.Context) (*Dashboard, error)
}
