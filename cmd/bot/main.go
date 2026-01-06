// cmd/bot/main.go
package main

import (
	"cashback-tracker/internal/config"
	"cashback-tracker/internal/domain"
	"cashback-tracker/internal/storage/postgres"
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"golang.org/x/text/encoding/charmap"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func SanitizeInput(s string) string {
	// Замени все пробельные символы на обычный пробел
	result := ""
	for _, r := range s {
		if unicode.IsSpace(r) {
			result += " "
		} else {
			result += string(r)
		}
	}
	// Убери лишние пробелы
	return strings.Join(strings.Fields(result), " ")
}

func main() {
	_ = godotenv.Load()

	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if botToken == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN not set")
	}

	cfg := config.MustLoad()
	db, err := pgxpool.New(context.Background(), cfg.DBConn)
	if err != nil {
		log.Fatal("Failed to connect to DB:", err)
	}
	defer db.Close()

	store := postgres.NewStorage(db)

	bot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		log.Panic(err)
	}

	log.Printf("Bot started: @%s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

for update := range updates {
	if update.Message == nil {
		continue
	}

	chatID := update.Message.Chat.ID
	userID := int64(update.Message.From.ID)

	rawText := update.Message.Text
	fixedText := fixEncoding(rawText)
	text := strings.TrimSpace(fixedText)

	log.Printf("📥 Received: %q (fixed from %q)", text, rawText)
	// text := strings.TrimSpace(update.Message.Text)

	var msgText string
	var err error

	log.Printf("📩 RAW TEXT HEX: % x", []byte(update.Message.Text))
	log.Printf("📩 RAW TEXT: %q", update.Message.Text)

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
		msgText, err = handleMonth(store, userID)

	case strings.HasPrefix(text, "/search_bank "):
		bankName := strings.TrimSpace(text[13:])
		msgText, err = handleSearchBank(store, userID, bankName)

	case strings.HasPrefix(text, "/search_cat "):
		catName := strings.TrimSpace(text[12:])
		msgText, err = handleSearchCategory(store, userID, catName)

	case strings.HasPrefix(text, "/delete_bank "):
		parts := strings.Split(text, " ")
	if len(parts) < 2 {
		msgText = "❌ Используй: /delete_bank Банк"
	} else {
		bankName := parts[1]
		err = handleDeleteBank(store, userID, bankName)
		if err == nil {
			msgText = "✅ Банк удалён"
		}
	}

	case strings.HasPrefix(text, "/delete_cat "):
		// Убираем "/delete_cat " (13 символов, но в байтах может быть больше!)
	// Лучше: делим по пробелам
	parts := strings.Split(text, " ")
	if len(parts) < 3 {
		msgText = "❌ Используй: /delete_cat Банк Категория"
	} else {
		bankName := parts[1]
		catName := strings.Join(parts[2:], " ")
		err = handleDeleteCategory(store, userID, bankName, catName)
		if err == nil {
			msgText = "✅ Категория удалена"
		}
	}

	case strings.HasPrefix(text, "/add"):
		// Оставляем старую логику
		if len(text) <= 4 {
			msgText = "Отправь категории в формате:\nСбер: Аптеки 5, Такси 10"
		} else {
			input := strings.TrimSpace(text[4:])
			err = saveFromMessage(store, userID, input)
			if err == nil {
				msgText = "✅ Сохранено!"
			}
		}

	default:
		msgText = "Неизвестная команда. Напиши /help"
	}

	if err != nil {
		msgText = "❌ Ошибка: " + err.Error()
	}

	msg := tgbotapi.NewMessage(chatID, msgText)
	msg.ParseMode = "Markdown" // для форматирования
	bot.Send(msg)
}
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