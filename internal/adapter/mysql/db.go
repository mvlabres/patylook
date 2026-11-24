// Package mysql implements the persistence ports on top of database/sql.
// It performs DML only: the schema is owned by the already existing database.
package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"

	"patylook/internal/core/port"
)

// Config holds the connection settings.
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	Name     string
	// Location is used to read/write TIMESTAMP values and as the session time zone.
	Location *time.Location
	// TLS is passed to the driver ("", "true", "skip-verify", "preferred").
	TLS string
}

// Open creates the connection pool and verifies connectivity.
func Open(ctx context.Context, cfg Config) (*sql.DB, error) {
	dc := mysql.NewConfig()
	dc.User = cfg.User
	dc.Passwd = cfg.Password
	dc.Net = "tcp"
	dc.Addr = fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	dc.DBName = cfg.Name
	dc.ParseTime = true
	dc.Loc = cfg.Location
	dc.Collation = "utf8mb4_unicode_ci"
	dc.TLSConfig = cfg.TLS
	dc.Timeout = 10 * time.Second
	dc.ReadTimeout = 30 * time.Second
	dc.WriteTimeout = 30 * time.Second
	// Keep CURRENT_TIMESTAMP and TIMESTAMP reads consistent with the application's time zone.
	_, offset := time.Now().In(cfg.Location).Zone()
	dc.Params = map[string]string{"time_zone": fmt.Sprintf("'%s'", formatOffset(offset))}

	db, err := sql.Open("mysql", dc.FormatDSN())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute) // shared hosts close idle connections aggressively
	db.SetConnMaxIdleTime(2 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, fmt.Errorf("conectar ao MySQL: %w", err)
	}
	return db, nil
}

func formatOffset(seconds int) string {
	sign := '+'
	if seconds < 0 {
		sign, seconds = '-', -seconds
	}
	return fmt.Sprintf("%c%02d:%02d", sign, seconds/3600, seconds%3600/60)
}

// querier is satisfied by both *sql.DB and *sql.Tx.
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type txKey struct{}

// base gives repositories access to the transaction carried by the context, if any.
type base struct{ db *sql.DB }

func (b base) q(ctx context.Context) querier {
	if tx, ok := ctx.Value(txKey{}).(*sql.Tx); ok {
		return tx
	}
	return b.db
}

// TxManager implements port.Transactor.
type TxManager struct{ base }

var _ port.Transactor = (*TxManager)(nil)

func NewTxManager(db *sql.DB) *TxManager { return &TxManager{base{db}} }

func (m *TxManager) WithinTx(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	if _, ok := ctx.Value(txKey{}).(*sql.Tx); ok {
		return fn(ctx) // join the outer transaction
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if err = fn(context.WithValue(ctx, txKey{}, tx)); err != nil {
		return err
	}
	return tx.Commit()
}
