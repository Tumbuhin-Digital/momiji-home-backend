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
// @securityDefinitions.apikey SessionAuth
// @in header
// @name X-Session-ID

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
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/dashboard"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/order"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/database"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/email"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/scheduler"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/server"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shipstation"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/preorder"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/preorderbatch"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/preordercustomtext"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/product"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/settings"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/warehouse"
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
	preorderBatchStore := preorderbatch.NewPostgresStore(db)
	customerStore := customer.NewPostgresStore(db)
	stockLockStore := checkout.NewPostgresStockLockStore(db)
	dashboardStore := dashboard.NewPostgresStore(db)
	settingsStore := settings.NewPostgresStore(db)
	warehouseStore := warehouse.NewPostgresStore(db)

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

	shipstationClient := shipstation.NewClient(cfg.ShipStation.APIKey, cfg.ShipStation.Sandbox)

	preorderCustomTextStore := preordercustomtext.NewPostgresStore(db)
	preorderCustomTextService := preordercustomtext.NewService(preorderCustomTextStore)
	preorderBatchService := preorderbatch.NewService(preorderBatchStore, productStore, preorderCustomTextService)
	batchLockService := preorderbatch.NewBatchLockService(preorderBatchStore, func(ctx context.Context, shopifyVariantID string) (string, error) {
		variant, err := productStore.GetVariantByShopifyID(ctx, shopifyVariantID)
		if err != nil {
			return "", err
		}
		if variant == nil {
			return "", nil
		}
		return variant.ID, nil
	})

	productService := product.NewProductService(productStore, shopifyClient, preorderCustomTextService)
	cartService := cart.NewCartService(cartStore, productService)
	if setter, ok := cartService.(interface{ SetBatchPreviewer(cart.BatchPreviewer) }); ok {
		setter.SetBatchPreviewer(preorderBatchService)
	}
	preorderService := preorder.NewPreorderService(preorderStore, orderStore, notificationService, shopifyClient, cfg.App.FrontendURL)
	warehouseService := warehouse.NewService(warehouseStore, cfg.ShipStation)
	if err := warehouseService.EnsureFromEnv(context.Background(), cfg.ShipStation); err != nil {
		log.Warn("Failed to ensure warehouse config from env", slog.Any("error", err))
	}

	orderService := order.NewOrderService(orderStore, cartService, authStore, shopifyClient, preorderStore, preorderService, notificationService, shipstationClient, cfg.ShipStation, warehouseService)
	if setter, ok := orderService.(interface {
		SetBatchAllocationReleaser(order.BatchAllocationReleaser)
	}); ok {
		setter.SetBatchAllocationReleaser(preorderBatchService)
	}
	customerService := customer.NewCustomerService(customerStore)
	stockLockService := checkout.NewStockLockService(stockLockStore, productService, shopifyClient)
	webhookService := webhook.NewWebhookService(orderStore, authStore, productStore, shopifyClient, preorderStore, notificationService, stockLockService, customerStore, orderService)
	if setter, ok := webhookService.(interface{ SetBatchAllocator(webhook.BatchAllocator) }); ok {
		setter.SetBatchAllocator(preorderBatchService)
	}
	if setter, ok := webhookService.(interface {
		SetBatchLockReleaser(webhook.BatchLockReleaser)
	}); ok {
		setter.SetBatchLockReleaser(batchLockService)
	}
	if setter, ok := webhookService.(interface {
		SetStockLockStore(checkout.StockLockStore)
	}); ok {
		setter.SetStockLockStore(stockLockStore)
	}
	dashboardService := dashboard.NewDashboardService(dashboardStore)
	settingsService := settings.NewSettingsService(settingsStore)

	// Initialize Fiber App
	app := server.NewFiberApp(log)
	api := app.Group("/api/v1")

	// Initialize Handlers & Routes
	secureCookie := cfg.App.Env == "production" || cfg.App.Env == "staging"
	authHandler := auth.NewAuthHandler(authService, cfg.Auth.Secret, secureCookie, cfg.Auth.EnablePublicRegister)
	authHandler.SetupRoutes(api)
	if cfg.Auth.EnablePublicRegister {
		log.Warn("Public /auth/register is enabled — disable ENABLE_PUBLIC_REGISTER in production")
	}

	cartHandler := cart.NewCartHandler(cartService, cfg.Auth.Secret)
	cartHandler.SetupRoutes(api)

	checkoutService := checkout.NewCheckoutService(cartService, productService, shopifyClient, stockLockService, stockLockStore, settingsStore, cfg.App.FrontendURL, shipstationClient, cfg.ShipStation, warehouseService)
	if setter, ok := checkoutService.(interface{ SetBatchPreviewer(checkout.BatchPreviewer) }); ok {
		setter.SetBatchPreviewer(preorderBatchService)
	}
	if setter, ok := checkoutService.(interface {
		SetBatchLockService(preorderbatch.BatchLockService)
	}); ok {
		setter.SetBatchLockService(batchLockService)
	}
	checkoutHandler := checkout.NewCheckoutHandler(cartService, checkoutService, orderService, productService, cfg.Auth.Secret)
	checkoutHandler.SetupRoutes(api)

	productHandler := product.NewProductHandler(productService, cfg.Auth.Secret)
	productHandler.SetBatchManager(preorderBatchService)
	productHandler.SetupRoutes(api)

	orderHandler := order.NewOrderHandler(orderService, cfg.Auth.Secret)
	orderHandler.SetupRoutes(api)

	preorderHandler := preorder.NewPreorderHandler(preorderService, cfg.Auth.Secret)
	preorderHandler.SetupRoutes(api)

	customerHandler := customer.NewCustomerHandler(customerService, cfg.Auth.Secret)
	customerHandler.SetupRoutes(api)

	webhookHandler := webhook.NewWebhookHandler(webhookService, cfg.Shopify.WebhookSecret)
	webhookHandler.SetupRoutes(app) // Root level for webhooks (no /api/v1 prefix)

	dashboardHandler := dashboard.NewHandler(dashboardService)
	dashboardHandler.SetupRoutes(api, cfg.Auth.Secret)

	settingsHandler := settings.NewSettingsHandler(settingsService, cfg.Auth.Secret)
	settingsHandler.SetupRoutes(api)

	preorderCustomTextHandler := preordercustomtext.NewHandler(preorderCustomTextService, cfg.Auth.Secret)
	preorderCustomTextHandler.SetupRoutes(api)

	preorderBatchHandler := preorderbatch.NewHandler(preorderBatchService, cfg.Auth.Secret)
	preorderBatchHandler.SetupRoutes(api)

	warehouseHandler := warehouse.NewHandler(warehouseService, cfg.Auth.Secret)
	warehouseHandler.SetupRoutes(api)

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
		_ = batchLockService.CleanExpiredBatchLocks(ctx)
	})

	// Start server in a separate goroutinee
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
