package mysql

import (
	"context"
	"database/sql"
	"fmt"
)

// minPasswordColumn is the width bcrypt hashes need (60 characters).
const minPasswordColumn = 60

// VerifySchema is a read-only sanity check of the existing database. It never changes it.
func VerifySchema(ctx context.Context, db *sql.DB) error {
	var width sql.NullInt64
	err := db.QueryRowContext(ctx,
		"SELECT CHARACTER_MAXIMUM_LENGTH FROM information_schema.COLUMNS "+
			"WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user' AND COLUMN_NAME = 'password'").Scan(&width)
	if err == sql.ErrNoRows {
		return fmt.Errorf("tabela `user` não encontrada no banco configurado (DB_NAME): confira o nome do banco")
	}
	if err != nil {
		return fmt.Errorf("verificar schema: %w", err)
	}
	if width.Valid && width.Int64 < minPasswordColumn {
		return fmt.Errorf("a coluna user.password tem %d caracteres e não comporta hashes bcrypt (%d); "+
			"aplique migrations/001_widen_user_password.sql", width.Int64, minPasswordColumn)
	}
	var n int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'product_images'").Scan(&n); err != nil {
		return fmt.Errorf("verificar schema: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("tabela product_images não encontrada; execute o bloco dela no schema.sql (somente esse bloco)")
	}
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'brand'").Scan(&n); err != nil {
		return fmt.Errorf("verificar schema: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("tabela brand não encontrada; execute o bloco dela no schema.sql (somente esse bloco)")
	}
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'product' AND COLUMN_NAME = 'brand_id'").Scan(&n); err != nil {
		return fmt.Errorf("verificar schema: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("a coluna product.brand_id não existe; aplique migrations/002_add_product_brand.sql")
	}
	return nil
}
