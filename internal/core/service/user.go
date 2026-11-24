package service

import (
	"context"

	"patylook/internal/core/domain"
	"patylook/internal/core/port"
)

type UserService struct {
	users    port.UserRepository
	sessions port.SessionStore
	hasher   port.PasswordHasher
	admin    adminRule
}

var _ port.UserService = (*UserService)(nil)

func NewUserService(users port.UserRepository, sessions port.SessionStore, hasher port.PasswordHasher, adminCategory string) *UserService {
	return &UserService{users: users, sessions: sessions, hasher: hasher, admin: adminRule(adminCategory)}
}

func requireAdmin(actor *domain.User) error {
	if actor == nil || !actor.Admin {
		return domain.ErrForbidden
	}
	return nil
}

var userConflicts = map[string]string{
	"username": "Já existe um usuário com esse login",
	"email":    "Já existe um usuário com esse e-mail",
}

func (s *UserService) List(ctx context.Context, actor *domain.User, page int, search string) (port.Page[domain.User], error) {
	if err := requireAdmin(actor); err != nil {
		return port.Page[domain.User]{}, err
	}
	page, lp := listParams(page, search)
	items, total, err := s.users.List(ctx, lp)
	if err != nil {
		return port.Page[domain.User]{}, err
	}
	return newPage(items, total, page), nil
}

func (s *UserService) Get(ctx context.Context, actor *domain.User, id int) (*domain.User, error) {
	if err := requireAdmin(actor); err != nil {
		return nil, err
	}
	u, err := s.users.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.admin.mark(u), nil
}

func (s *UserService) Categories(ctx context.Context) ([]domain.UserCategory, error) {
	return s.users.ListCategories(ctx)
}

func (s *UserService) Create(ctx context.Context, actor *domain.User, u *domain.User, password string) error {
	if err := requireAdmin(actor); err != nil {
		return err
	}
	return s.create(ctx, u, password)
}

func (s *UserService) create(ctx context.Context, u *domain.User, password string) error {
	u.Normalize()
	v := domain.NewValidationError()
	if err := u.Validate(); err != nil {
		mergeValidation(v, err)
	}
	if err := domain.ValidatePassword("password", password); err != nil {
		mergeValidation(v, err)
	}
	if err := v.OrNil(); err != nil {
		return err
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return err
	}
	u.PasswordHash = hash
	u.IsActive = true
	return conflictToValidation(s.users.Create(ctx, u), userConflicts)
}

// Bootstrap creates the first administrator when the user table is empty.
// It reports whether a user was created.
func (s *UserService) Bootstrap(ctx context.Context, username, name, password string) (bool, error) {
	n, err := s.users.Count(ctx)
	if err != nil || n > 0 {
		return false, err
	}
	catID, err := s.users.EnsureCategory(ctx, string(s.admin))
	if err != nil {
		return false, err
	}
	u := &domain.User{Username: username, Name: name, CategoryID: catID}
	if err := s.create(ctx, u, password); err != nil {
		return false, err
	}
	return true, nil
}

func (s *UserService) Update(ctx context.Context, actor *domain.User, u *domain.User) error {
	if err := requireAdmin(actor); err != nil {
		return err
	}
	u.Normalize()
	if err := u.Validate(); err != nil {
		return err
	}
	current, err := s.users.FindByID(ctx, u.ID)
	if err != nil {
		return err
	}
	if u.ID == actor.ID { // prevents an admin from locking themselves out
		u.CategoryID = current.CategoryID
	}
	return conflictToValidation(s.users.Update(ctx, u), userConflicts)
}

func (s *UserService) ResetPassword(ctx context.Context, actor *domain.User, id int, password string) error {
	if err := requireAdmin(actor); err != nil {
		return err
	}
	if err := domain.ValidatePassword("password", password); err != nil {
		return err
	}
	if _, err := s.users.FindByID(ctx, id); err != nil {
		return err
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return err
	}
	if err := s.users.UpdatePassword(ctx, id, hash); err != nil {
		return err
	}
	return s.sessions.DeleteByUser(ctx, id)
}

func (s *UserService) SetActive(ctx context.Context, actor *domain.User, id int, active bool) error {
	if err := requireAdmin(actor); err != nil {
		return err
	}
	if id == actor.ID && !active {
		v := domain.NewValidationError()
		v.Add("", "Você não pode desativar o próprio usuário")
		return v
	}
	if err := s.users.SetActive(ctx, id, active); err != nil {
		return err
	}
	if !active {
		return s.sessions.DeleteByUser(ctx, id)
	}
	return nil
}

func (s *UserService) ChangeOwnPassword(ctx context.Context, actor *domain.User, current, next string) error {
	u, err := s.users.FindByID(ctx, actor.ID)
	if err != nil {
		return err
	}
	v := domain.NewValidationError()
	if !s.hasher.Verify(u.PasswordHash, current) {
		v.Add("current_password", "Senha atual incorreta")
	}
	if err := domain.ValidatePassword("new_password", next); err != nil {
		mergeValidation(v, err)
	}
	if err := v.OrNil(); err != nil {
		return err
	}
	hash, err := s.hasher.Hash(next)
	if err != nil {
		return err
	}
	if err := s.users.UpdatePassword(ctx, u.ID, hash); err != nil {
		return err
	}
	return s.sessions.DeleteByUser(ctx, u.ID) // forces re-login on every device
}

// mergeValidation copies the messages of err into v when err is a *ValidationError.
func mergeValidation(v *domain.ValidationError, err error) {
	if ve, ok := err.(*domain.ValidationError); ok {
		for f, m := range ve.Fields {
			v.Add(f, m)
		}
	}
}
