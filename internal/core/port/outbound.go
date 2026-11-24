package port

import (
	"context"
	"io"
	"time"

	"patylook/internal/core/domain"
)

// ListParams carries search and pagination options for list queries.
type ListParams struct {
	Search string
	Limit  int
	Offset int
	// OnlyActive restricts product listings to active products (ignored by other repositories).
	OnlyActive bool
}

// Page is a slice of a larger result set.
type Page[T any] struct {
	Items    []T
	Total    int
	Page     int
	PageSize int
}

// Pages returns the total number of pages.
func (p Page[T]) Pages() int {
	if p.PageSize <= 0 {
		return 1
	}
	n := (p.Total + p.PageSize - 1) / p.PageSize
	if n < 1 {
		return 1
	}
	return n
}

// Transactor runs fn inside a database transaction. Repositories called with the
// context passed to fn participate in that transaction.
type Transactor interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}

type UserRepository interface {
	Count(ctx context.Context) (int, error)
	FindByID(ctx context.Context, id int) (*domain.User, error)
	FindByUsername(ctx context.Context, username string) (*domain.User, error)
	List(ctx context.Context, p ListParams) ([]domain.User, int, error)
	Create(ctx context.Context, u *domain.User) error
	Update(ctx context.Context, u *domain.User) error
	UpdatePassword(ctx context.Context, id int, hash string) error
	SetActive(ctx context.Context, id int, active bool) error
	ListCategories(ctx context.Context) ([]domain.UserCategory, error)
	// EnsureCategory returns the id of the named category, creating it if missing.
	EnsureCategory(ctx context.Context, name string) (int, error)
}

type ProductCategoryRepository interface {
	List(ctx context.Context) ([]domain.ProductCategory, error)
	FindByID(ctx context.Context, id int) (*domain.ProductCategory, error)
	Create(ctx context.Context, c *domain.ProductCategory) error
	Update(ctx context.Context, c *domain.ProductCategory) error
	Delete(ctx context.Context, id int) error
}

type ProductRepository interface {
	FindByID(ctx context.Context, id int) (*domain.Product, error)
	// List returns products with their price valid on the given date.
	List(ctx context.Context, p ListParams, on time.Time) ([]domain.Product, int, error)
	// ListSellable returns active products with stock, used by the order form.
	ListSellable(ctx context.Context, on time.Time) ([]domain.Product, error)
	Create(ctx context.Context, p *domain.Product) error
	Update(ctx context.Context, p *domain.Product) error
	SetActive(ctx context.Context, id int, active bool) error
	// DecrementStock fails with domain.ErrInsufficientStock when stock < qty.
	DecrementStock(ctx context.Context, id, qty int) error
	IncrementStock(ctx context.Context, id, qty int) error
	CountLowStock(ctx context.Context, threshold int) (int, error)
}

type PricebookRepository interface {
	ListByProduct(ctx context.Context, productID int) ([]domain.Pricebook, error)
	// FindCurrent returns the entry valid on the date, or domain.ErrNotFound.
	FindCurrent(ctx context.Context, productID int, on time.Time) (*domain.Pricebook, error)
	Create(ctx context.Context, p *domain.Pricebook) error
}

type CustomerRepository interface {
	FindByID(ctx context.Context, id int) (*domain.Customer, error)
	List(ctx context.Context, p ListParams) ([]domain.Customer, int, error)
	ListActive(ctx context.Context) ([]domain.Customer, error)
	Create(ctx context.Context, c *domain.Customer) error
	Update(ctx context.Context, c *domain.Customer) error
	SetActive(ctx context.Context, id int, active bool) error
}

type BrandRepository interface {
	FindByID(ctx context.Context, id int) (*domain.Brand, error)
	List(ctx context.Context, p ListParams) ([]domain.Brand, int, error)
	ListActive(ctx context.Context) ([]domain.Brand, error)
	Create(ctx context.Context, b *domain.Brand) error
	Update(ctx context.Context, b *domain.Brand) error
	SetActive(ctx context.Context, id int, active bool) error
}

type PaymentTypeRepository interface {
	List(ctx context.Context) ([]domain.PaymentType, error)
	FindByID(ctx context.Context, id int) (*domain.PaymentType, error)
	Create(ctx context.Context, p *domain.PaymentType) error
	Update(ctx context.Context, p *domain.PaymentType) error
	Delete(ctx context.Context, id int) error
}

// OrderFilter restricts order listings.
type OrderFilter struct {
	ListParams // Search matches customer name or order id
	Status     domain.OrderStatus
}

// OrderSummary aggregates orders for the dashboard.
type OrderSummary struct {
	OpenOrders      int
	CompletedOrders int
	CompletedTotal  domain.Money
}

type OrderRepository interface {
	// Create inserts the order and its items, setting o.ID.
	Create(ctx context.Context, o *domain.Order) error
	FindByID(ctx context.Context, id int) (*domain.Order, error)
	List(ctx context.Context, f OrderFilter) ([]domain.Order, int, error)
	// UpdateStatus changes the status only if it is currently `from`;
	// otherwise it returns domain.ErrConflict.
	UpdateStatus(ctx context.Context, id int, from, to domain.OrderStatus) error
	// Summary aggregates orders created at or after since.
	Summary(ctx context.Context, since time.Time) (OrderSummary, error)
	Recent(ctx context.Context, limit int) ([]domain.Order, error)
}

// SessionStore keeps server-side sessions keyed by the hash of the cookie token.
type SessionStore interface {
	Create(ctx context.Context, s domain.Session) error
	Find(ctx context.Context, tokenHash string) (*domain.Session, error)
	Update(ctx context.Context, s domain.Session) error
	Delete(ctx context.Context, tokenHash string) error
	DeleteByUser(ctx context.Context, userID int) error
	DeleteExpired(ctx context.Context, now time.Time) (int, error)
}

type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(hash, password string) bool
}

// ProductImageRepository persists the registered image paths of products.
type ProductImageRepository interface {
	// ListByProduct returns the images ordered main first, then by sort order.
	ListByProduct(ctx context.Context, productID int) ([]domain.ProductImage, error)
	ListByProducts(ctx context.Context, productIDs []int) (map[int][]domain.ProductImage, error)
	FindByID(ctx context.Context, id int) (*domain.ProductImage, error)
	Create(ctx context.Context, img *domain.ProductImage) error
	Delete(ctx context.Context, id int) error
}

// ImageStorage keeps image files, addressed by product folder and file name.
type ImageStorage interface {
	// Save writes (or atomically replaces) a file, creating the folder when needed.
	Save(ctx context.Context, folder, name string, data []byte) error
	// Remove deletes a file; a missing file is not an error. Empty folders are removed too.
	Remove(ctx context.Context, folder, name string) error
	Open(ctx context.Context, folder, name string) (io.ReadSeekCloser, time.Time, error)
}
