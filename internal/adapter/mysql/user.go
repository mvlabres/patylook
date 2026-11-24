package mysql

import (
	"context"
	"database/sql"
	"errors"

	"patylook/internal/core/domain"
	"patylook/internal/core/port"
)

type UserRepository struct{ base }

var _ port.UserRepository = (*UserRepository)(nil)

func NewUserRepository(db *sql.DB) *UserRepository { return &UserRepository{base{db}} }

const userSelect = "SELECT u.id, u.username, COALESCE(u.email, ''), u.name, u.password, " +
	"u.user_category_id, uc.name, u.is_active, u.created_at " +
	"FROM `user` u JOIN user_category uc ON uc.id = u.user_category_id "

func scanUser(s scanner) (*domain.User, error) {
	var (
		u       domain.User
		created sql.NullTime
	)
	err := s.Scan(&u.ID, &u.Username, &u.Email, &u.Name, &u.PasswordHash,
		&u.CategoryID, &u.CategoryName, &u.IsActive, &created)
	if err != nil {
		return nil, err
	}
	u.CreatedAt = timeOrZero(created)
	return &u, nil
}

func (r *UserRepository) Count(ctx context.Context) (int, error) {
	var n int
	err := r.q(ctx).QueryRowContext(ctx, "SELECT COUNT(*) FROM `user`").Scan(&n)
	return n, err
}

func (r *UserRepository) FindByID(ctx context.Context, id int) (*domain.User, error) {
	u, err := scanUser(r.q(ctx).QueryRowContext(ctx, userSelect+"WHERE u.id = ?", id))
	return u, translate(err)
}

func (r *UserRepository) FindByUsername(ctx context.Context, username string) (*domain.User, error) {
	u, err := scanUser(r.q(ctx).QueryRowContext(ctx, userSelect+"WHERE u.username = ?", username))
	return u, translate(err)
}

func (r *UserRepository) List(ctx context.Context, p port.ListParams) ([]domain.User, int, error) {
	where, args := "", []any{}
	if p.Search != "" {
		like := likePattern(p.Search)
		where = "WHERE (u.username LIKE ? OR u.name LIKE ? OR u.email LIKE ?) "
		args = append(args, like, like, like)
	}
	var total int
	count := "SELECT COUNT(*) FROM `user` u " + where
	if err := r.q(ctx).QueryRowContext(ctx, count, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.q(ctx).QueryContext(ctx, userSelect+where+"ORDER BY u.name LIMIT ? OFFSET ?",
		append(args, p.Limit, p.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []domain.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *u)
	}
	return out, total, rows.Err()
}

func (r *UserRepository) Create(ctx context.Context, u *domain.User) error {
	res, err := r.q(ctx).ExecContext(ctx,
		"INSERT INTO `user` (username, email, password, user_category_id, name, is_active) VALUES (?, ?, ?, ?, ?, ?)",
		u.Username, nullString(u.Email), u.PasswordHash, u.CategoryID, u.Name, u.IsActive)
	if err != nil {
		return translate(err)
	}
	id, err := res.LastInsertId()
	u.ID = int(id)
	return err
}

func (r *UserRepository) Update(ctx context.Context, u *domain.User) error {
	_, err := r.q(ctx).ExecContext(ctx,
		"UPDATE `user` SET username = ?, email = ?, name = ?, user_category_id = ? WHERE id = ?",
		u.Username, nullString(u.Email), u.Name, u.CategoryID, u.ID)
	return translate(err)
}

func (r *UserRepository) UpdatePassword(ctx context.Context, id int, hash string) error {
	_, err := r.q(ctx).ExecContext(ctx, "UPDATE `user` SET password = ? WHERE id = ?", hash, id)
	return translate(err)
}

func (r *UserRepository) SetActive(ctx context.Context, id int, active bool) error {
	_, err := r.q(ctx).ExecContext(ctx, "UPDATE `user` SET is_active = ? WHERE id = ?", active, id)
	return translate(err)
}

func (r *UserRepository) ListCategories(ctx context.Context) ([]domain.UserCategory, error) {
	rows, err := r.q(ctx).QueryContext(ctx, "SELECT id, name FROM user_category ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.UserCategory
	for rows.Next() {
		var c domain.UserCategory
		if err := rows.Scan(&c.ID, &c.Name); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *UserRepository) EnsureCategory(ctx context.Context, name string) (int, error) {
	var id int
	err := r.q(ctx).QueryRowContext(ctx, "SELECT id FROM user_category WHERE name = ?", name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	res, err := r.q(ctx).ExecContext(ctx, "INSERT INTO user_category (name) VALUES (?)", name)
	if err != nil {
		return 0, translate(err)
	}
	n, err := res.LastInsertId()
	return int(n), err
}
