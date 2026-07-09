package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

type Config struct {
	App         AppConfig
	Database    DatabaseConfig
	Auth        AuthConfig
	Shopify     ShopifyConfig
	Email       EmailConfig
	ShipStation ShipStationConfig
}

type AppConfig struct {
	Env         string
	Port        string
	Host        string
	FrontendURL string
}

type DatabaseConfig struct {
	Adapter           string `yaml:"adapter"`
	Pool              int    `yaml:"pool"`
	IdleTimeout       string `yaml:"idle_timeout"`
	ConnectionTimeout string `yaml:"connection_timeout"`
	Host              string
	Port              string
	Database          string
	Username          string
	Password          string
}

type AuthConfig struct {
	JWT struct {
		AccessTokenTTL  string `yaml:"access_token_ttl"`
		RefreshTokenTTL string `yaml:"refresh_token_ttl"`
		BcryptCost      int    `yaml:"bcrypt_cost"`
	} `yaml:"jwt"`
	RateLimit struct {
		MaxAttempts int    `yaml:"max_attempts"`
		Window      string `yaml:"window"`
	} `yaml:"rate_limit"`
	Secret               string
	RefreshSecret        string
	EnablePublicRegister bool
}

type ShopifyConfig struct {
	StoreDomain     string
	AdminAPIToken   string
	StorefrontToken string
	WebhookSecret   string
}

type EmailConfig struct {
	SMTPHost     string
	SMTPPort     int
	SMTPUser     string
	SMTPPassword string
	From         string
}

type ShipStationConfig struct {
	APIKey            string
	Sandbox           bool
	WarehouseZip      string
	WarehouseCountry  string
	WarehouseName     string
	WarehousePhone    string
	WarehouseAddress1 string
	WarehouseCity     string
	WarehouseState    string
	CarrierCodes      []string
	GroundServiceCode string
}

func Load() (*Config, error) {
	_ = godotenv.Load() // ignore error if .env doesn't exist

	env := os.Getenv("APP_ENV")
	if env == "" {
		env = "development"
	}

	cfg := &Config{
		App: AppConfig{
			Env:         env,
			Port:        os.Getenv("PORT"),
			Host:        os.Getenv("APP_HOST"),
			FrontendURL: os.Getenv("FE_URL"),
		},
	}
	if cfg.App.Port == "" {
		cfg.App.Port = "3000"
	}
	if cfg.App.Host == "" {
		cfg.App.Host = "localhost:" + cfg.App.Port
	}

	if err := loadYAML("config/database.yaml", env, &cfg.Database); err != nil {
		return nil, fmt.Errorf("failed to load database config: %w", err)
	}
	cfg.Database.Host = os.Getenv("DB_HOST")
	cfg.Database.Port = os.Getenv("DB_PORT")
	if cfg.Database.Port == "" {
		cfg.Database.Port = "5432"
	}
	cfg.Database.Database = os.Getenv("DB_NAME")
	cfg.Database.Username = os.Getenv("DB_USERNAME")
	if cfg.Database.Username == "" {
		cfg.Database.Username = os.Getenv("DB_USER")
	}
	cfg.Database.Password = os.Getenv("DB_PASSWORD")

	if err := loadYAML("config/auth.yaml", "", &cfg.Auth); err != nil {
		return nil, fmt.Errorf("failed to load auth config: %w", err)
	}
	cfg.Auth.Secret = os.Getenv("JWT_SECRET")
	cfg.Auth.RefreshSecret = os.Getenv("JWT_REFRESH_SECRET")
	if v := strings.TrimSpace(os.Getenv("ENABLE_PUBLIC_REGISTER")); v != "" {
		enabled, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("invalid ENABLE_PUBLIC_REGISTER value %q: %w", v, err)
		}
		cfg.Auth.EnablePublicRegister = enabled
	}

	cfg.Shopify.StoreDomain = os.Getenv("SHOPIFY_STORE_DOMAIN")
	cfg.Shopify.AdminAPIToken = os.Getenv("SHOPIFY_ADMIN_API_TOKEN")
	cfg.Shopify.StorefrontToken = os.Getenv("SHOPIFY_STOREFRONT_TOKEN")
	cfg.Shopify.WebhookSecret = os.Getenv("SHOPIFY_WEBHOOK_SECRET")

	cfg.Email.SMTPHost = os.Getenv("SMTP_HOST")
	var port int
	fmt.Sscanf(os.Getenv("SMTP_PORT"), "%d", &port)
	if port == 0 {
		port = 465
	}
	cfg.Email.SMTPPort = port
	cfg.Email.SMTPUser = os.Getenv("SMTP_USER")
	cfg.Email.SMTPPassword = os.Getenv("SMTP_PASS")
	cfg.Email.From = os.Getenv("EMAIL_FROM")

	cfg.ShipStation.APIKey = os.Getenv("SHIPSTATION_API_KEY")
	cfg.ShipStation.Sandbox = cfg.App.Env != "production"
	cfg.ShipStation.WarehouseZip = os.Getenv("WAREHOUSE_ZIP")
	cfg.ShipStation.WarehouseCountry = os.Getenv("WAREHOUSE_COUNTRY")
	cfg.ShipStation.WarehouseName = os.Getenv("WAREHOUSE_NAME")
	cfg.ShipStation.WarehousePhone = os.Getenv("WAREHOUSE_PHONE")
	cfg.ShipStation.WarehouseAddress1 = os.Getenv("WAREHOUSE_ADDRESS1")
	cfg.ShipStation.WarehouseCity = os.Getenv("WAREHOUSE_CITY")
	cfg.ShipStation.WarehouseState = os.Getenv("WAREHOUSE_STATE")

	carrierCodesStr := os.Getenv("SHIPSTATION_CARRIER_CODES")
	if carrierCodesStr != "" {
		for _, code := range strings.Split(carrierCodesStr, ",") {
			cfg.ShipStation.CarrierCodes = append(cfg.ShipStation.CarrierCodes, strings.TrimSpace(code))
		}
	}

	cfg.ShipStation.GroundServiceCode = os.Getenv("SHIPSTATION_GROUND_SERVICE_CODE")
	if cfg.ShipStation.GroundServiceCode == "" {
		cfg.ShipStation.GroundServiceCode = "ups_ground"
	}

	return cfg, nil
}

func loadYAML(filename string, envNode string, target interface{}) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	if envNode != "" {
		// Parse into a map of maps and extract the specific environment
		var raw map[string]interface{}
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return err
		}

		envData, ok := raw[envNode]
		if !ok {
			return fmt.Errorf("environment '%s' not found in config file", envNode)
		}

		// Re-marshal and unmarshal into the target struct
		bytes, err := yaml.Marshal(envData)
		if err != nil {
			return err
		}
		return yaml.Unmarshal(bytes, target)
	}

	return yaml.Unmarshal(data, target)
}
