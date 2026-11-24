// Command patylook runs the Patylook order management web application.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/fcgi"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // time zones even on hosts without a tz database

	"patylook/internal/adapter/filestore"
	"patylook/internal/adapter/memory"
	"patylook/internal/adapter/mysql"
	"patylook/internal/adapter/security"
	"patylook/internal/adapter/web"
	"patylook/internal/config"
	"patylook/internal/core/service"
	assets "patylook/web"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "hash-password" {
		if err := hashPassword(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "erro:", err)
			os.Exit(1)
		}
		return
	}

	envFile := flag.String("env", ".env", "optional file with KEY=VALUE settings")
	flag.Parse()

	if err := run(*envFile); err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
}

func run(envFile string) error {
	cfg, err := config.Load(envFile)
	if err != nil {
		return fmt.Errorf("configuração: %w", err)
	}
	log := newLogger(cfg)
	slog.SetDefault(log)

	if cfg.IsProduction() && cfg.DBUser == "root" && cfg.DBPassword == "" {
		log.Warn("APP_ENV=production com as credenciais LOCAIS do banco (root, sem senha): altere DB_HOST, DB_USER, DB_PASSWORD e DB_NAME")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := mysql.Open(ctx, mysql.Config{
		Host: cfg.DBHost, Port: cfg.DBPort, User: cfg.DBUser, Password: cfg.DBPassword,
		Name: cfg.DBName, Location: cfg.Location, TLS: cfg.DBTLS,
	})
	if err != nil {
		return err
	}
	defer db.Close()
	if err := mysql.VerifySchema(ctx, db); err != nil {
		return err
	}

	// Adapters
	now := service.Clock(func() time.Time { return time.Now().In(cfg.Location) })
	hasher := security.NewBcryptHasher()
	sessions := memory.NewSessionStore()
	tx := mysql.NewTxManager(db)
	userRepo := mysql.NewUserRepository(db)
	categoryRepo := mysql.NewProductCategoryRepository(db)
	brandRepo := mysql.NewBrandRepository(db)
	productRepo := mysql.NewProductRepository(db, cfg.Location)
	priceRepo := mysql.NewPricebookRepository(db)
	customerRepo := mysql.NewCustomerRepository(db)
	paymentRepo := mysql.NewPaymentTypeRepository(db)
	orderRepo := mysql.NewOrderRepository(db)
	imageRepo := mysql.NewProductImageRepository(db)
	imageStore, err := filestore.New(cfg.ImagesDir)
	if err != nil {
		return err
	}
	log.Info("pasta de imagens", "dir", imageStore.Root())
	// Application services
	auth, err := service.NewAuthService(userRepo, sessions, hasher,
		service.AuthConfig{SessionTTL: cfg.SessionTTL, AdminCategory: cfg.AdminCategory}, now)
	if err != nil {
		return err
	}
	users := service.NewUserService(userRepo, sessions, hasher, cfg.AdminCategory)

	if cfg.BootstrapUsername != "" && cfg.BootstrapPassword != "" {
		created, err := users.Bootstrap(ctx, cfg.BootstrapUsername, cfg.BootstrapName, cfg.BootstrapPassword)
		if err != nil {
			return fmt.Errorf("criar administrador inicial: %w", err)
		}
		if created {
			log.Info("administrador inicial criado; remova BOOTSTRAP_ADMIN_* da configuração", "username", cfg.BootstrapUsername)
		}
	}

	server, err := web.NewServer(web.Services{
		Auth:      auth,
		Users:     users,
		Catalog:   service.NewCatalogService(categoryRepo, brandRepo, productRepo, priceRepo, imageRepo, imageStore, tx, now, log),
		Customers: service.NewCustomerService(customerRepo),
		Brands:    service.NewBrandService(brandRepo),
		Payments:  service.NewPaymentTypeService(paymentRepo),
		Orders:    service.NewOrderService(orderRepo, productRepo, priceRepo, customerRepo, paymentRepo, tx, now),
		Dashboard: service.NewDashboardService(orderRepo, productRepo, now),
	}, web.Options{
		Assets:       assets.Files,
		Location:     cfg.Location,
		CookieSecure: cfg.CookieSecure,
		Logger:       log,
	})
	if err != nil {
		return err
	}
	handler, err := server.Handler()
	if err != nil {
		return err
	}

	go cleanupSessions(ctx, auth, log)

	if cfg.Mode == "fcgi" {
		log.Info("iniciando em modo FastCGI")
		return fcgi.Serve(nil, handler) // listens on the descriptor provided by the web server
	}
	return serveHTTP(ctx, cfg.Addr, handler, log)
}

func serveHTTP(ctx context.Context, addr string, handler http.Handler, log *slog.Logger) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	log.Info("servidor iniciado", "addr", ln.Addr().String())

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("encerrando servidor")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

func cleanupSessions(ctx context.Context, auth *service.AuthService, log *slog.Logger) {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n, err := auth.CleanupExpired(ctx); err != nil {
				log.Warn("limpeza de sessões", "err", err)
			} else if n > 0 {
				log.Debug("sessões expiradas removidas", "count", n)
			}
		}
	}
}

func newLogger(cfg config.Config) *slog.Logger {
	if cfg.IsProduction() {
		return slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
}
