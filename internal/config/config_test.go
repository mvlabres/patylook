package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"APP_ENV", "APP_MODE", "APP_ADDR", "PORT", "DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME", "DB_TLS",
		"APP_TIMEZONE", "SESSION_TTL", "COOKIE_SECURE", "ADMIN_CATEGORY",
		"BOOTSTRAP_ADMIN_USERNAME", "BOOTSTRAP_ADMIN_NAME", "BOOTSTRAP_ADMIN_PASSWORD",
	} {
		t.Setenv(k, "") // registers restoration
		os.Unsetenv(k)
	}
}

func TestLoadDefaultsAndEnvFile(t *testing.T) {
	clearEnv(t)
	file := filepath.Join(t.TempDir(), ".env")
	content := "# comment\nDB_USER=root\nDB_PASSWORD=\"p@ss word\"\nDB_NAME='shop'\nSESSION_TTL=30m\n"
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DB_NAME", "from-real-env") // real environment wins over the file

	cfg, err := Load(file)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DBUser != "root" || cfg.DBPassword != "p@ss word" || cfg.DBName != "from-real-env" {
		t.Errorf("db settings: %+v", cfg)
	}
	if cfg.Addr != ":8080" || cfg.Mode != "http" || cfg.DBPort != 3306 || cfg.AdminCategory != "admin" {
		t.Errorf("defaults: %+v", cfg)
	}
	if cfg.SessionTTL != 30*time.Minute || cfg.Location.String() != "America/Sao_Paulo" {
		t.Errorf("ttl/location: %v %v", cfg.SessionTTL, cfg.Location)
	}
	if cfg.CookieSecure {
		t.Error("cookies must not be Secure by default in development (plain http on localhost)")
	}
}

func TestLoadProduction(t *testing.T) {
	clearEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("DB_USER", "u")
	t.Setenv("PORT", "9000")
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.env"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.CookieSecure || cfg.Addr != ":9000" {
		t.Errorf("production defaults: secure=%v addr=%s", cfg.CookieSecure, cfg.Addr)
	}
}

func TestLoadValidation(t *testing.T) {
	clearEnv(t)
	t.Setenv("APP_MODE", "cgi")
	t.Setenv("DB_PORT", "abc")
	t.Setenv("SESSION_TTL", "forever")
	t.Setenv("APP_TIMEZONE", "Mars/Olympus")
	if _, err := Load(filepath.Join(t.TempDir(), "missing.env")); err == nil {
		t.Fatal("expected configuration errors")
	} else {
		for _, want := range []string{"APP_MODE", "DB_PORT", "SESSION_TTL", "APP_TIMEZONE", "DB_USER"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error should mention %s: %v", want, err)
			}
		}
	}
}
