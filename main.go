// cmd/api/main.go
package main

import (
	"cashback-tracker/internal/auth"
	"cashback-tracker/internal/config"
	"cashback-tracker/internal/handler"
	"cashback-tracker/internal/middleware"
	"cashback-tracker/internal/storage/postgres"
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {

	// Настройка логгера
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug, // в проде → slog.LevelInfo
	}))
	slog.SetDefault(logger) // делаем глобальным

	cfg := config.MustLoad()

	pool, err := pgxpool.New(context.Background(), cfg.DBConn)
	if err != nil {
		log.Fatalf("Не удалось подключиться к БД: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(context.Background()); err != nil {
		log.Fatalf("Ping БД не удался: %v", err)
	}
	log.Println("✅ Подключились к PostgreSQL")

	store := postgres.NewStorage(pool)

	// JWT
	tokenService := auth.NewTokenService(cfg)
	authMiddleware := middleware.NewAuthMiddleware(tokenService)

	router := gin.Default()

	router.POST("/api/v1/login", func(c *gin.Context) {
	var req struct {
		UserID int `json:"user_id" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id required"})
		return
	}
	token, err := tokenService.GenerateToken(req.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "token generation failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token})
})

	// Роуты
	// router.GET("/health", func(c *gin.Context) {
	// 	c.JSON(http.StatusOK, gin.H{"status": "ok"})
	// })

	cashbackHandler := handler.NewCashbackHandler(store)
	v1 := router.Group("/api/v1")
	v1.Use(authMiddleware.RequireAuth())
	{
		v1.POST("/month", cashbackHandler.SaveMonth)
		v1.GET("/month", cashbackHandler.GetMonth)
		v1.GET("/search/category", cashbackHandler.SearchByCategory)
		v1.GET("/search/bank", cashbackHandler.SearchByBank)
		v1.PUT("/month", cashbackHandler.SaveMonth)    // полная замена
		v1.PATCH("/month", cashbackHandler.PatchMonth) // частичное обновление
		v1.DELETE("/month/bank", cashbackHandler.DeleteBankFromMonth)
		v1.DELETE("/month/bank/category", cashbackHandler.DeleteCategoryFromBank)
	}

	// 🔥 ОБЯЗАТЕЛЬНО: запускаем сервер
	log.Printf("🚀 Сервер запущен на http://localhost%s", cfg.ServerPort)
	if err := router.Run(cfg.ServerPort); err != nil {
		log.Fatalf("Сервер завершил работу с ошибкой: %v", err)
	}
}
