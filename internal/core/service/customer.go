package service

import (
	"context"

	"patylook/internal/core/domain"
	"patylook/internal/core/port"
)

type CustomerService struct {
	customers port.CustomerRepository
}

var _ port.CustomerService = (*CustomerService)(nil)

func NewCustomerService(c port.CustomerRepository) *CustomerService {
	return &CustomerService{customers: c}
}

var customerConflicts = map[string]string{"document": "Já existe um cliente com esse CPF"}

func (s *CustomerService) List(ctx context.Context, page int, search string) (port.Page[domain.Customer], error) {
	page, lp := listParams(page, search)
	items, total, err := s.customers.List(ctx, lp)
	if err != nil {
		return port.Page[domain.Customer]{}, err
	}
	return newPage(items, total, page), nil
}

func (s *CustomerService) Get(ctx context.Context, id int) (*domain.Customer, error) {
	return s.customers.FindByID(ctx, id)
}

func (s *CustomerService) Save(ctx context.Context, c *domain.Customer) error {
	c.Normalize()
	if err := c.Validate(); err != nil {
		return err
	}
	var err error
	if c.ID == 0 {
		c.IsActive = true
		err = s.customers.Create(ctx, c)
	} else {
		err = s.customers.Update(ctx, c)
	}
	return conflictToValidation(err, customerConflicts)
}

func (s *CustomerService) SetActive(ctx context.Context, id int, active bool) error {
	return s.customers.SetActive(ctx, id, active)
}

type PaymentTypeService struct {
	types port.PaymentTypeRepository
}

var _ port.PaymentTypeService = (*PaymentTypeService)(nil)

func NewPaymentTypeService(r port.PaymentTypeRepository) *PaymentTypeService {
	return &PaymentTypeService{types: r}
}

var paymentTypeConflicts = map[string]string{"name": "Já existe uma forma de pagamento com esse nome"}

func (s *PaymentTypeService) List(ctx context.Context) ([]domain.PaymentType, error) {
	return s.types.List(ctx)
}

func (s *PaymentTypeService) Get(ctx context.Context, id int) (*domain.PaymentType, error) {
	return s.types.FindByID(ctx, id)
}

func (s *PaymentTypeService) Save(ctx context.Context, p *domain.PaymentType) error {
	p.Normalize()
	if err := p.Validate(); err != nil {
		return err
	}
	var err error
	if p.ID == 0 {
		err = s.types.Create(ctx, p)
	} else {
		err = s.types.Update(ctx, p)
	}
	return conflictToValidation(err, paymentTypeConflicts)
}

func (s *PaymentTypeService) Delete(ctx context.Context, id int) error {
	return s.types.Delete(ctx, id)
}
