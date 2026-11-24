// Package config loads runtime settings from environment variables, optionally
// seeded from a .env file (handy for localhost; on the server use real env vars).
package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env  string // "development" | "production"
	Mode string // "http" | "fcgi"
	Addr string // listen address for http mode

	DBHost     string
	DBPort     int
	DBUser     string
	DBPassword string
	DBName     string
	DBTLS      string

	Location      *time.Location
	SessionTTL    time.Duration
	CookieSecure  bool
	AdminCategory string
	// ImagesDir is the directory holding one folder per product (images/products/<SKU>).
	ImagesDir string

	// Optional first-run bootstrap, used only when the user table is empty.
	BootstrapUsername string
	BootstrapName     string
	BootstrapPassword string
}

func (c Config) IsProduction() bool { return c.Env == "production" }

// Load reads configuration, applying .env (if present) without overriding real environment variables.
func Load(envFile string) (Config, error) {
	if err := loadEnvFile(envFile); err != nil {
		return Config{}, err
	}

	var errs []error
	get := func(key, def string) string {
		if v, ok := os.LookupEnv(key); ok && v != "" {
			return v
		}
		return def
	}
	getInt := func(key string, def int) int {
		v := get(key, "")
		if v == "" {
			return def
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s inválido: %q", key, v))
			return def
		}
		return n
	}

	cfg := Config{
		Env:           strings.ToLower(get("APP_ENV", "development")),
		Mode:          strings.ToLower(get("APP_MODE", "http")),
		DBHost:        get("DB_HOST", "127.0.0.1"),
		DBPort:        getInt("DB_PORT", 3306),
		DBUser:        get("DB_USER", ""),
		DBPassword:    get("DB_PASSWORD", ""),
		DBName:        get("DB_NAME", "patylook"),
		DBTLS:         get("DB_TLS", ""),
		AdminCategory: get("ADMIN_CATEGORY", "admin"),
		ImagesDir:     get("IMAGES_DIR", "images/products"),

		BootstrapUsername: get("BOOTSTRAP_ADMIN_USERNAME", ""),
		BootstrapName:     get("BOOTSTRAP_ADMIN_NAME", "Administrador"),
		BootstrapPassword: get("BOOTSTRAP_ADMIN_PASSWORD", ""),
	}

	// PORT is honored for platforms/hosts that inject it; APP_ADDR wins when set.
	if port := get("PORT", ""); port != "" {
		cfg.Addr = ":" + port
	}
	cfg.Addr = get("APP_ADDR", orDefault(cfg.Addr, ":8080"))

	tz := get("APP_TIMEZONE", "America/Sao_Paulo")
	loc, err := time.LoadLocation(tz)
	if err != nil {
		errs = append(errs, fmt.Errorf("APP_TIMEZONE inválido %q: %w", tz, err))
		loc = time.UTC
	}
	cfg.Location = loc

	ttl, err := time.ParseDuration(get("SESSION_TTL", "8h"))
	if err != nil || ttl <= 0 {
		errs = append(errs, fmt.Errorf("SESSION_TTL inválido (ex.: 8h, 30m)"))
		ttl = 8 * time.Hour
	}
	cfg.SessionTTL = ttl

	secure := get("COOKIE_SECURE", "")
	if secure == "" {
		cfg.CookieSecure = cfg.IsProduction()
	} else if cfg.CookieSecure, err = strconv.ParseBool(secure); err != nil {
		errs = append(errs, fmt.Errorf("COOKIE_SECURE inválido: %q", secure))
	}

	if cfg.Mode != "http" && cfg.Mode != "fcgi" {
		errs = append(errs, fmt.Errorf("APP_MODE deve ser http ou fcgi"))
	}
	if cfg.DBUser == "" {
		errs = append(errs, errors.New("DB_USER é obrigatório"))
	}
	return cfg, errors.Join(errs...)
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// loadEnvFile sets variables from KEY=VALUE lines. A missing file is not an error.
func loadEnvFile(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if len(val) >= 2 && (val[0] == '"' && val[len(val)-1] == '"' || val[0] == '\'' && val[len(val)-1] == '\'') {
			val = val[1 : len(val)-1]
		}
		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, val); err != nil {
				return err
			}
		}
	}
	return sc.Err()
}
