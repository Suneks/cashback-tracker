// internal/config/config.go
package config

import (
	"os"
	"time"
)

type Config struct {
	ServerPort   string
	DBConn       string
	JWTSecret    string
	JWTExpiresIn time.Duration
}

func MustLoad() Config {
	dbConn := os.Getenv("DATABASE_URL")
	if dbConn == "" {
		dbConn = "postgres://postgres:postgres@localhost:5432/cashback?sslmode=disable"
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// ✅ JWT-настройки
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "your-super-secret-jwt-key-change-in-prod"
	}

	jwtExpiresIn := 24 * time.Hour
	if expiresInStr := os.Getenv("JWT_EXPIRES_IN"); expiresInStr != "" {
		if d, err := time.ParseDuration(expiresInStr); err == nil {
			jwtExpiresIn = d
		}
	}

	// ✅ ОДИН return в конце
	return Config{
		ServerPort:   ":" + port,
		DBConn:       dbConn,
		JWTSecret:    jwtSecret,
		JWTExpiresIn: jwtExpiresIn,
	}
}