// cmd/migrate/main.go
package main

import (
	"database/sql"
	"log/slog"
	"os"

	"github.com/pressly/goose/v3"
	_ "github.com/jackc/pgx/v5/stdlib" // ← регистрирует драйвер "pgx"
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

	if err := goose.Up(db, "migrations"); err != nil {
		slog.Error("Миграции завершились с ошибкой", "error", err)
		os.Exit(1)
	}

	slog.Info("✅ Миграции применены")
}