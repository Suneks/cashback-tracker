// cmd/migrate/main.go
package main

import (
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"github.com/pressly/goose/v3"
	_ "github.com/jackc/pgx/v5/stdlib" 
)

func main() {
	conn := os.Getenv("DATABASE_URL")
	if conn == "" {
		conn = "postgres://postgres:postgres@localhost:5432/cashback?sslmode=disable"
	}

	db, err := sql.Open("pgx", conn)
	if err != nil {
		slog.Error("Не удалось открыть БД", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// 🔥 Используем текущую рабочую директорию
	wd, err := os.Getwd()
	if err != nil {
		slog.Error("Не удалось получить рабочую директорию", "error", err)
		os.Exit(1)
	}

	migrationsDir := filepath.Join(wd, "migrations")

	slog.Info("Применяем миграции", "dir", migrationsDir)

	if err := goose.Up(db, migrationsDir); err != nil {
		slog.Error("Миграции завершились с ошибкой", "error", err)
		os.Exit(1)
	}

	slog.Info("✅ Миграции применены")
}