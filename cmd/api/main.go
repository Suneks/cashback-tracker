// cmd/api/main.go
package main

import (
	"cashback-tracker/internal/auth"
	"cashback-tracker/internal/config"
	"cashback-tracker/internal/domain"
	"cashback-tracker/internal/handler"
	"cashback-tracker/internal/middleware"
	"cashback-tracker/internal/storage/postgres"
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/text/encoding/charmap"
)

func main() {
	// Настройка логгера
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	cfg := config.MustLoad()

	pool, err := pgxpool.New(context.Background(), cfg.DBConn)
	if err != nil {
		slog.Error("Не удалось подключиться к БД", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	store := postgres.NewStorage(pool)

	// JWT
	tokenService := auth.NewTokenService(cfg)

	// Gin
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	// Health check
	router.GET("/health", func(c * gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Telegram webhook
	// Telegram webhook
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if botToken != "" {
	bot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		slog.Error("Не удалось инициализировать Telegram бота", "error", err)
		os.Exit(1)
	}

	// Устанавливаем webhook через MakeRequest
	webhookURL := os.Getenv("RENDER_EXTERNAL_URL") + "/telegram"
	if _, err := bot.MakeRequest("setWebhook", map[string]string{"url": webhookURL}); err != nil {
		slog.Error("Не удалось установить webhook", "error", err)
		os.Exit(1)
	}
	slog.Info("Telegram webhook установлен", "url", webhookURL)

		// Обработка входящих сообщений
	router.POST("/telegram", func(c *gin.Context) {
		var update tgbotapi.Update
		if err := c.ShouldBindJSON(&update); err != nil {
			slog.Error("Ошибка парсинга обновления", "error", err)
			c.Status(http.StatusBadRequest)
			return
		}
		if update.Message == nil {
			c.Status(http.StatusOK)
			return
		}

			chatID := update.Message.Chat.ID
			userID := int64(update.Message.From.ID)
			text := strings.TrimSpace(update.Message.Text)
			slog.Info("📥 Получено сообщение", "user_id", userID, "text", text)

			var msgText string
			var errHandle error

			switch {
			case text == "/start" || text == "/help":
				msgText = "🏦 *Кэшбэк-трекер*\n\n" +
					"Команды:\n" +
					"`/add` — добавить банк: `Сбер: Аптеки 5, Такси 10`\n" +
					"`/month` — показать кэшбэк за текущий месяц\n" +
					"`/search_bank Сбер` — найти категории по банку\n" +
					"`/search_cat Аптеки` — найти банки по категории\n" +
					"`/delete_bank Сбер` — удалить банк\n" +
					"`/delete_cat Сбер Аптеки` — удалить категорию"

			case text == "/month":
				msgText, errHandle = handleMonth(store, userID)

			case strings.HasPrefix(text, "/search_bank "):
				bankName := strings.TrimSpace(text[13:])
				msgText, errHandle = handleSearchBank(store, userID, bankName)

			case strings.HasPrefix(text, "/search_cat "):
				catName := strings.TrimSpace(text[12:])
				msgText, errHandle = handleSearchCategory(store, userID, catName)

			case strings.HasPrefix(text, "/delete_bank "):
				bankName := strings.TrimSpace(text[14:])
				parts := strings.Split(text, " ")
				if len(parts) < 2 {
					msgText = "❌ Используй: /delete_bank Банк"
				} else {
					bankName = parts[1]
					errHandle = handleDeleteBank(store, userID, bankName)
					if errHandle == nil {
						msgText = "✅ Банк удалён"
					}
				}

			case strings.HasPrefix(text, "/delete_cat "):
				parts := strings.Split(text, " ")
				if len(parts) < 3 {
					msgText = "❌ Используй: /delete_cat Банк Категория"
				} else {
					bankName := parts[1]
					catName := strings.Join(parts[2:], " ")
					errHandle = handleDeleteCategory(store, userID, bankName, catName)
					if errHandle == nil {
						msgText = "✅ Категория удалена"
					}
				}

			case strings.HasPrefix(text, "/add "):
				input := strings.TrimSpace(text[5:])
				errHandle = saveFromMessage(store, userID, input)
				if errHandle == nil {
					msgText = "✅ Сохранено!"
				}

			default:
				msgText = "Неизвестная команда. Напиши /help"
			}

			if errHandle != nil {
				msgText = "❌ Ошибка: " + errHandle.Error()
			}

			// Отправляем ответ
			msg := tgbotapi.NewMessage(chatID, msgText)
			msg.ParseMode = "Markdown"
			_, _ = bot.Send(msg)

			c.Status(http.StatusOK)
		})
	}

	// API-эндпоинты
	router.POST("/api/v1/login", func(c *gin.Context) {
		var req struct {
			UserID int64 `json:"user_id" binding:"required,min=1"`
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

	authMiddleware := middleware.NewAuthMiddleware(tokenService)
	v1 := router.Group("/api/v1")
	v1.Use(authMiddleware.RequireAuth())
	{
		v1.POST("/month", cashbackHandler(store).SaveMonth)
		v1.GET("/month", cashbackHandler(store).GetMonth)
		v1.GET("/search/category", cashbackHandler(store).SearchByCategory)
		v1.GET("/search/bank", cashbackHandler(store).SearchByBank)
		v1.PUT("/month", cashbackHandler(store).SaveMonth)
		v1.PATCH("/month", cashbackHandler(store).PatchMonth)
		v1.DELETE("/month/bank", cashbackHandler(store).DeleteBankFromMonth)
		v1.DELETE("/month/bank/category", cashbackHandler(store).DeleteCategoryFromBank)
	}

	// Запуск сервера
	port := os.Getenv("PORT")
	if port == "" {
		port = "10000"
	}
	slog.Info("🚀 Сервер запущен", "port", port)
	if err := router.Run(":" + port); err != nil {
		slog.Error("Сервер завершил работу с ошибкой", "error", err)
	}
}

// --- ФУНКЦИИ ОБРАБОТКИ ДЛЯ БОТА (копируем из cmd/bot/main.go) ---

func cashbackHandler(store any) *handler.CashbackHandler {
	// Обход типизации для краткости
	return handler.NewCashbackHandler(store.(handler.CombinedStorage))
}

func saveFromMessage(store *postgres.Storage, userID int64, input string) error {
	if !strings.Contains(input, ":") {
		return fmt.Errorf("используй формат: Банк: Категория1 5, Категория2 10")
	}

	parts := strings.SplitN(input, ":", 2)
	bankName := strings.TrimSpace(parts[0])
	categoriesStr := strings.TrimSpace(parts[1])

	if bankName == "" || categoriesStr == "" {
		return fmt.Errorf("банк и категории не могут быть пустыми")
	}

	var categories []domain.CashbackCategory
	for _, catPart := range strings.Split(categoriesStr, ",") {
		catPart = strings.TrimSpace(catPart)
		fields := strings.Fields(catPart)
		if len(fields) < 2 {
			return fmt.Errorf("категория должна содержать название и процент: %q", catPart)
		}

		percentStr := fields[len(fields)-1]
		percent, err := strconv.ParseFloat(percentStr, 32)
		if err != nil {
			return fmt.Errorf("неверный процент: %q", percentStr)
		}

		catName := strings.Join(fields[:len(fields)-1], " ")
		if catName == "" {
			return fmt.Errorf("название категории не может быть пустым")
		}

		categories = append(categories, domain.CashbackCategory{
			Category: domain.Category{Name: catName},
			Percent:  float32(percent),
		})
	}

	if len(categories) == 0 {
		return fmt.Errorf("не найдено ни одной валидной категории")
	}

	month := time.Now().Format("2006-01")
	bankWithCat := []domain.BankWithCategories{{
		Bank:       domain.Bank{Name: bankName},
		Categories: categories,
	}}

	return store.PatchMonth(context.Background(), userID, month, bankWithCat)
}

// ... остальные функции handleMonth, handleSearchBank и т.д. (скопируй их из cmd/bot/main.go) ...

func handleMonth(store *postgres.Storage, userID int64) (string, error) {
	month := time.Now().Format("2006-01")
	cashback, err := store.GetMonth(context.Background(), userID, month)
	if err != nil {
		return "", err
	}
	if cashback == nil || len(cashback.Banks) == 0 {
		return "📭 Нет данных за " + month, nil
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("🏦 *Кэшбэк за %s*", month))
	for _, bwc := range cashback.Banks {
		lines = append(lines, fmt.Sprintf("\n*%s*", bwc.Bank.Name))
		for _, cc := range bwc.Categories {
			lines = append(lines, fmt.Sprintf("- %s: %.1f%%", cc.Category.Name, cc.Percent))
		}
	}
	return strings.Join(lines, "\n"), nil
}

func handleSearchBank(store *postgres.Storage, userID int64, bankName string) (string, error) {
	if bankName == "" {
		return "❌ Укажи название банка", nil
	}
	month := time.Now().Format("2006-01")
	
	// Получаем ВЕСЬ месяц
	cashback, err := store.GetMonth(context.Background(), userID, month)
	if err != nil {
		return "", err
	}
	if cashback == nil || len(cashback.Banks) == 0 {
		return fmt.Sprintf("📭 Нет данных за %s", month), nil
	}

	// Ищем нужный банк
	var targetBank *domain.BankWithCategories
	for _, bwc := range cashback.Banks {
		if strings.EqualFold(bwc.Bank.Name, bankName) {
			targetBank = &bwc
			break
		}
	}

	if targetBank == nil {
		return fmt.Sprintf("📭 Нет кэшбэка по банку *%s*", bankName), nil
	}

	// Формируем ответ с процентами
	var lines []string
	lines = append(lines, fmt.Sprintf("🔍 *Категории для %s*", bankName))
	for _, cc := range targetBank.Categories {
		lines = append(lines, fmt.Sprintf("- %s: %.1f%%", cc.Category.Name, cc.Percent))
	}
	return strings.Join(lines, "\n"), nil
}

func handleSearchCategory(store *postgres.Storage, userID int64, categoryName string) (string, error) {
	if categoryName == "" {
		return "❌ Укажи название категории", nil
	}
	month := time.Now().Format("2006-01")
	
	cashback, err := store.GetMonth(context.Background(), userID, month)
	if err != nil {
		return "", err
	}
	if cashback == nil || len(cashback.Banks) == 0 {
		return fmt.Sprintf("📭 Нет данных за %s", month), nil
	}

	var banksWithCategory []domain.BankWithCategories
	for _, bwc := range cashback.Banks {
		for _, cc := range bwc.Categories {
			if strings.EqualFold(cc.Category.Name, categoryName) {
				banksWithCategory = append(banksWithCategory, domain.BankWithCategories{
					Bank: bwc.Bank,
					Categories: []domain.CashbackCategory{cc}, // только нужная категория
				})
				break
			}
		}
	}

	if len(banksWithCategory) == 0 {
		return fmt.Sprintf("📭 Нет кэшбэка по категории *%s*", categoryName), nil
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("🔍 *Банки с кэшбэком по %s*", categoryName))
	for _, bwc := range banksWithCategory {
		cc := bwc.Categories[0]
		lines = append(lines, fmt.Sprintf("- %s: %.1f%%", bwc.Bank.Name, cc.Percent))
	}
	return strings.Join(lines, "\n"), nil
}

func handleDeleteBank(store *postgres.Storage, userID int64, bankName string) error {
	if bankName == "" {
		return fmt.Errorf("укажи название банка")
	}
	month := time.Now().Format("2006-01")
	return store.DeleteBankFromMonth(context.Background(), userID, month, bankName)
}

func handleDeleteCategory(store *postgres.Storage, userID int64, bankName, categoryName string) error {
	if bankName == "" || categoryName == "" {
		return fmt.Errorf("укажи банк и категорию")
	}
	month := time.Now().Format("2006-01")
	log.Printf("🗑️ Удаляем категорию: bank='%s', category='%s'", bankName, categoryName)

	return store.DeleteCategoryFromBank(context.Background(), userID, month, bankName, categoryName)
}


func fixEncoding(s string) string {
	// Проверим, является ли строка валидной UTF-8
	if utf8.ValidString(s) {
		return s
	}

	// Пробуем перекодировать из windows-1251
	decoder := charmap.Windows1251.NewDecoder()
	fixed, err := decoder.String(s)
	if err == nil && utf8.ValidString(fixed) {
		return fixed
	}

	// Если не получилось — заменяем невалидные символы
	return strings.ToValidUTF8(s, "")
}