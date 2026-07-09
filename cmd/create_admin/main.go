package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/auth"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/config"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/database"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	email := flag.String("email", "", "admin email (required)")
	password := flag.String("password", "", "admin password, min 8 chars (required)")
	flag.Parse()

	*email = strings.TrimSpace(*email)
	if *email == "" || *password == "" {
		fmt.Fprintln(os.Stderr, "usage: go run cmd/create_admin/main.go -email=admin@example.com -password=secretpass")
		os.Exit(2)
	}
	if len(*password) < 8 {
		log.Fatal("password must be at least 8 characters")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	db, err := database.NewPostgresDB(cfg.Database)
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("get sql db: %v", err)
	}
	defer sqlDB.Close()

	store := auth.NewPostgresAuthStore(db)
	ctx := context.Background()

	existing, err := store.GetUserByEmail(ctx, *email)
	if err != nil {
		log.Fatalf("lookup email: %v", err)
	}
	if existing != nil {
		log.Fatalf("email already registered: %s (id=%s role=%s)", existing.Email, existing.ID, existing.Role)
	}

	cost := cfg.Auth.JWT.BcryptCost
	if cost < 10 {
		cost = 12
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(*password), cost)
	if err != nil {
		log.Fatalf("hash password: %v", err)
	}

	user := &auth.User{
		Email:        *email,
		PasswordHash: string(hashed),
		Role:         "admin",
	}
	if err := store.CreateUser(ctx, user); err != nil {
		log.Fatalf("create user: %v", err)
	}

	fmt.Printf("Admin created: id=%s email=%s role=%s\n", user.ID, user.Email, user.Role)
}
