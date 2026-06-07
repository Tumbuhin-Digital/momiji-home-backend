package main

// @title Momiji Home API
// @version 1.0
// @description Momiji Home Backend API with Shopify orchestration
// @termsOfService http://swagger.io/terms/
// @host localhost:3000
// @BasePath /api/v1
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tumbuhindigi-sys/momiji-home-backend/docs"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/auth"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/cart"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/checkout"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/config"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/customer"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/order"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/database"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/email"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/scheduler"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/server"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/preorder"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/product"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/webhook"
)

func main() {
	// Initialize logger
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(log)

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Error("Failed to load configuration", slog.Any("error", err))
		os.Exit(1)
	}

	// Update Swagger Host based on environment
	docs.SwaggerInfo.Host = cfg.App.Host
	if cfg.App.Env == "production" || cfg.App.Env == "staging" {
		docs.SwaggerInfo.Schemes = []string{"https", "http"}
	} else {
		docs.SwaggerInfo.Schemes = []string{"http", "https"}
	}

	// Initialize Database
	db, err := database.NewPostgresDB(cfg.Database)
	if err != nil {
		log.Error("Failed to connect to database", slog.Any("error", err))
		os.Exit(1)
	}

	sqlDB, err := db.DB()
	if err != nil {
		log.Error("Failed to get generic database object", slog.Any("error", err))
		os.Exit(1)
	}
	defer sqlDB.Close()

	log.Info("Connected to database successfully")

	// Initialize Stores
	authStore := auth.NewPostgresAuthStore(db)
	cartStore := cart.NewPostgresCartStore(db)
	productStore := product.NewPostgresStore(db)
	orderStore := order.NewPostgresStore(db)
	preorderStore := preorder.NewPostgresPreorderStore(db)
	customerStore := customer.NewPostgresStore(db)
	stockLockStore := checkout.NewPostgresStockLockStore(db)

	// Initialize Services
	authService := auth.NewAuthService(authStore, cfg.Auth)

	shopifyClient := shopify.NewClient(
		cfg.Shopify.StoreDomain,
		cfg.Shopify.AdminAPIToken,
		cfg.Shopify.StorefrontToken,
	)

	var emailClient email.EmailClient
	if cfg.App.Env == "test" {
		emailClient = email.NewMockEmailClient()
	} else {
		emailClient = email.NewSMTPClient(
			cfg.Email.SMTPHost,
			cfg.Email.SMTPPort,
			cfg.Email.SMTPUser,
			cfg.Email.SMTPPassword,
			cfg.Email.From,
		)
	}
	notificationService := email.NewNotificationService(emailClient, "internal/platform/email/templates")

	productService := product.NewProductService(productStore, shopifyClient)
	cartService := cart.NewCartService(cartStore, productService)
	orderService := order.NewOrderService(orderStore, cartService, authStore, shopifyClient, preorderStore, notificationService)
	preorderService := preorder.NewPreorderService(preorderStore, orderStore, notificationService, shopifyClient)
	customerService := customer.NewCustomerService(customerStore)
	stockLockService := checkout.NewStockLockService(stockLockStore, productService)
	webhookService := webhook.NewWebhookService(orderStore, authStore, productStore, shopifyClient, preorderStore, notificationService, stockLockService)

	// Initialize Fiber App
	app := server.NewFiberApp(log)
	api := app.Group("/api/v1")

	// Initialize Handlers & Routes
	secureCookie := cfg.App.Env == "production" || cfg.App.Env == "staging"
	authHandler := auth.NewAuthHandler(authService, cfg.Auth.Secret, secureCookie)
	authHandler.SetupRoutes(api)

	cartHandler := cart.NewCartHandler(cartService, cfg.Auth.Secret)
	cartHandler.SetupRoutes(api)

	checkoutService := checkout.NewCheckoutService(cartService, shopifyClient, stockLockService, cfg.App.FrontendURL)
	checkoutHandler := checkout.NewCheckoutHandler(cartService, checkoutService, orderService, cfg.Auth.Secret)
	checkoutHandler.SetupRoutes(api)

	productHandler := product.NewProductHandler(productService, cfg.Auth.Secret)
	productHandler.SetupRoutes(api)

	orderHandler := order.NewOrderHandler(orderService, cfg.Auth.Secret)
	orderHandler.SetupRoutes(api)

	preorderHandler := preorder.NewPreorderHandler(preorderService, cfg.Auth.Secret)
	preorderHandler.SetupRoutes(api)

	customerHandler := customer.NewCustomerHandler(customerService, cfg.Auth.Secret)
	customerHandler.SetupRoutes(api)

	webhookHandler := webhook.NewWebhookHandler(webhookService, cfg.Shopify.WebhookSecret)
	webhookHandler.SetupRoutes(app) // Root level for webhooks (no /api/v1 prefix)

	// Start scheduled jobs
	scheduler.StartDailyJob(context.Background(), func(ctx context.Context) {
		log.Info("Running daily preorder reminders")
		_ = preorderService.ProcessReminders(ctx)
		
		log.Info("Running nightly Shopify product sync")
		_ = productService.SyncFromShopify(ctx)
	})

	scheduler.StartIntervalJob(context.Background(), 5*time.Minute, func(ctx context.Context) {
		log.Info("Cleaning expired stock locks")
		_ = stockLockService.CleanExpiredLocks(ctx)
	})

	// Start server in a separate goroutine
	go func() {
		if err := app.Listen(":" + cfg.App.Port); err != nil {
			log.Error("Server failed", slog.Any("error", err))
		}
	}()

	log.Info("Server started", slog.String("port", cfg.App.Port))

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	<-quit
	log.Info("Shutting down server...")

	_, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := app.Shutdown(); err != nil {
		log.Error("Server forced to shutdown", slog.Any("error", err))
	}

	log.Info("Server exiting")
}
