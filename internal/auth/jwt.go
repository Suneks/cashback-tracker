// internal/auth/jwt.go
package auth

import (
	"cashback-tracker/internal/config"
	"errors"
	"log/slog"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type TokenService struct {
	secretKey []byte
	expiresIn time.Duration
}

func NewTokenService(cfg config.Config) *TokenService {
	return &TokenService{
		secretKey: []byte(cfg.JWTSecret),
		expiresIn: cfg.JWTExpiresIn,
	}
}

// Генерация токена
func (s *TokenService) GenerateToken(userID int) (string, error) {
	expTime := time.Now().Add(s.expiresIn)
	claims := jwt.MapClaims{
		"user_id": userID,
		"exp":     expTime.Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString(s.secretKey)
	if err == nil {
		slog.Info("JWT generated", "user_id", userID, "expires_at", expTime.Format("2006-01-02 15:04:05"))
	}
	return tokenStr, err
}

// Парсинг токена
func (s *TokenService) ParseToken(tokenStr string) (int64, error) { // ← возвращаем int64
	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return s.secretKey, nil
	})
	if err != nil {
		return 0, err
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		if userIDFloat, ok := claims["user_id"].(float64); ok {
			userID := int64(userIDFloat)
			if userID <= 0 {
				return 0, errors.New("invalid user_id")
			}
			slog.Debug("JWT parsed successfully", "user_id", userID)
			return userID, nil
		}
	}
	return 0, errors.New("invalid token claims")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}