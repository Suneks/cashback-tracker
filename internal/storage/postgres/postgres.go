// internal/storage/postgres/postgres.go
package postgres

import (
	"cashback-tracker/internal/domain"
	"context"
	"fmt"
	"log"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Storage struct {
	db *pgxpool.Pool
}

func NewStorage(db *pgxpool.Pool) *Storage {
	return &Storage{db: db}
}

// sanitizeString очищает строку от невидимых и проблемных символов
func sanitizeString(s string) string {
	// Удаляем/заменяем проблемные символы
	result := make([]rune, 0, len(s))
	for _, r := range s {
		// Заменяем NO-BREAK SPACE и другие непечатаемые символы на обычный пробел
		if unicode.IsSpace(r) {
			result = append(result, ' ')
		} else if r >= 32 && r <= 126 { // ASCII printable
			result = append(result, r)
		} else if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '.' || r == ',' {
			result = append(result, r)
		}
		// Игнорируем всё остальное (включая 0xa1, 0x92 и т.д.)
	}

	// Убираем лишние пробелы
	clean := strings.Join(strings.Fields(string(result)), " ")
	return clean
}

// === BankStorage ===

func (s *Storage) CreateIfNotExists(ctx context.Context, name string) (int, error) {
	var id int
	err := s.db.QueryRow(ctx, `
		INSERT INTO banks (name) 
		VALUES ($1) 
		ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, name).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create or get bank: %w", err)
	}
	return id, nil
}

func (s *Storage) FindByName(ctx context.Context, name string) (*domain.Bank, error) {
	var bank domain.Bank
	err := s.db.QueryRow(ctx, "SELECT id, name FROM banks WHERE name = $1", name).Scan(&bank.ID, &bank.Name)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("find bank: %w", err)
	}
	return &bank, nil
}

// === CategoryStorage ===

func (s *Storage) CreateCategoryIfNotExists(ctx context.Context, name string) (int, error) {
	var id int
	err := s.db.QueryRow(ctx, `
		INSERT INTO categories (name)
		VALUES ($1)
		ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, name).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create or get category: %w", err)
	}
	return id, nil
}

func (s *Storage) FindCategoryByName(ctx context.Context, name string) (*domain.Category, error) {
	var cat domain.Category
	err := s.db.QueryRow(ctx, "SELECT id, name FROM categories WHERE name = $1", name).Scan(&cat.ID, &cat.Name)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("find category: %w", err)
	}
	return &cat, nil
}

// === CashbackStorage ===

func (s *Storage) SaveMonth(ctx context.Context, userID int64, monthStr string, bankCategories []domain.BankWithCategories) error {
	for _, bc := range bankCategories {
		if strings.TrimSpace(bc.Bank.Name) == "" {
			return fmt.Errorf("bank name cannot be empty")
		}
		if len(bc.Categories) == 0 {
			return fmt.Errorf("bank %q must have at least one category", bc.Bank.Name)
		}
		for _, cc := range bc.Categories {
			if strings.TrimSpace(cc.Category.Name) == "" {
				return fmt.Errorf("category name cannot be empty for bank %q", bc.Bank.Name)
			}
			if cc.Percent < 0 || cc.Percent > 100 {
				return fmt.Errorf("percent must be between 0 and 100 for category %q", cc.Category.Name)
			}
		}
	}

	monthTime, err := time.Parse("2006-01", monthStr)
	if err != nil {
		return fmt.Errorf("invalid month format, expected YYYY-MM: %w", err)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, "DELETE FROM cashback_months WHERE user_id = $1 AND month = $2", userID, monthTime)
	if err != nil {
		return fmt.Errorf("clear old month: %w", err)
	}

	var monthID int
	err = tx.QueryRow(ctx, `
		INSERT INTO cashback_months (user_id, month) VALUES ($1, $2) RETURNING id
	`, userID, monthTime).Scan(&monthID)
	if err != nil {
		return fmt.Errorf("insert cashback_month: %w", err)
	}

	createBankInTx := func(name string) (int, error) {
		var id int
		err := tx.QueryRow(ctx, `
			INSERT INTO banks (name) VALUES ($1)
			ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
			RETURNING id
		`, name).Scan(&id)
		return id, err
	}

	createCategoryInTx := func(name string) (int, error) {
		var id int
		err := tx.QueryRow(ctx, `
			INSERT INTO categories (name) VALUES ($1)
			ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
			RETURNING id
		`, name).Scan(&id)
		return id, err
	}

	for _, bc := range bankCategories {
		bankID, err := createBankInTx(bc.Bank.Name)
		if err != nil {
			return fmt.Errorf("create bank %q: %w", bc.Bank.Name, err)
		}

		for _, cc := range bc.Categories {
			categoryID, err := createCategoryInTx(cc.Category.Name)
			if err != nil {
				return fmt.Errorf("create category %q: %w", cc.Category.Name, err)
			}

			_, err = tx.Exec(ctx, `
				INSERT INTO bank_cashback_categories (cashback_month_id, bank_id, category_id, percent)
				VALUES ($1, $2, $3, $4)
				ON CONFLICT (cashback_month_id, bank_id, category_id) 
				DO UPDATE SET percent = EXCLUDED.percent
			`, monthID, bankID, categoryID, cc.Percent)
			if err != nil {
				return fmt.Errorf("link bank-category: %w", err)
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	slog.Debug("SaveMonth completed", "user_id", userID, "month", monthStr)
	return nil
}

func (s *Storage) GetMonth(ctx context.Context, userID int64, monthStr string) (*domain.CashbackMonth, error) {
	monthTime, err := time.Parse("2006-01", monthStr)
	if err != nil {
		return nil, fmt.Errorf("invalid month format: %w", err)
	}

	var monthID int
	err = s.db.QueryRow(ctx, `
		SELECT id FROM cashback_months
		WHERE user_id = $1 AND month = $2
	`, userID, monthTime).Scan(&monthID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("find month: %w", err)
	}

	rows, err := s.db.Query(ctx, `
		SELECT 
			b.id, b.name,
			c.id, c.name,
			bcc.percent
		FROM bank_cashback_categories bcc
		JOIN banks b ON b.id = bcc.bank_id
		JOIN categories c ON c.id = bcc.category_id
		WHERE bcc.cashback_month_id = $1
		ORDER BY b.name, c.name
	`, monthID)
	if err != nil {
		return nil, fmt.Errorf("query bank-category: %w", err)
	}
	defer rows.Close()

	bankMap := make(map[int]*domain.BankWithCategories)
	for rows.Next() {
		var bankID, catID int
		var bankName, catName string
		var percent float64

		if err := rows.Scan(&bankID, &bankName, &catID, &catName, &percent); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}

		if _, exists := bankMap[bankID]; !exists {
			bankMap[bankID] = &domain.BankWithCategories{
				Bank: domain.Bank{ID: bankID, Name: bankName},
			}
		}
		bankMap[bankID].Categories = append(bankMap[bankID].Categories, domain.CashbackCategory{
			Category: domain.Category{ID: catID, Name: catName},
			Percent:  float32(percent),
		})
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	banks := make([]domain.BankWithCategories, 0, len(bankMap))
	for _, bwc := range bankMap {
		banks = append(banks, *bwc)
	}

	return &domain.CashbackMonth{
		Month:  monthStr,
		UserID: userID,
		Banks:  banks,
	}, nil
}

func (s *Storage) SearchByCategory(ctx context.Context, userID int64, monthStr, categoryName string) ([]domain.Bank, error) {
	monthTime, err := time.Parse("2006-01", monthStr)
	if err != nil {
		return nil, fmt.Errorf("invalid month: %w", err)
	}

	rows, err := s.db.Query(ctx, `
		SELECT DISTINCT b.id, b.name
		FROM bank_cashback_categories bcc
		JOIN banks b ON b.id = bcc.bank_id
		JOIN categories c ON c.id = bcc.category_id
		JOIN cashback_months cm ON cm.id = bcc.cashback_month_id
		WHERE cm.user_id = $1 AND cm.month = $2 AND c.name ILIKE $3
		ORDER BY b.name
	`, userID, monthTime, categoryName)
	if err != nil {
		return nil, fmt.Errorf("search banks by category: %w", err)
	}
	defer rows.Close()

	var banks []domain.Bank
	for rows.Next() {
		var bank domain.Bank
		if err := rows.Scan(&bank.ID, &bank.Name); err != nil {
			return nil, fmt.Errorf("scan bank: %w", err)
		}
		banks = append(banks, bank)
	}
	return banks, rows.Err()
}

func (s *Storage) SearchByBank(ctx context.Context, userID int64, monthStr, bankName string) ([]domain.Category, error) {
	monthTime, err := time.Parse("2006-01", monthStr)
	if err != nil {
		return nil, fmt.Errorf("invalid month: %w", err)
	}

	rows, err := s.db.Query(ctx, `
		SELECT DISTINCT c.id, c.name
		FROM bank_cashback_categories bcc
		JOIN banks b ON b.id = bcc.bank_id
		JOIN categories c ON c.id = bcc.category_id
		JOIN cashback_months cm ON cm.id = bcc.cashback_month_id
		WHERE cm.user_id = $1 AND cm.month = $2 AND b.name ILIKE $3
		ORDER BY c.name
	`, userID, monthTime, bankName)
	if err != nil {
		return nil, fmt.Errorf("search categories by bank: %w", err)
	}
	defer rows.Close()

	var categories []domain.Category
	for rows.Next() {
		var cat domain.Category
		if err := rows.Scan(&cat.ID, &cat.Name); err != nil {
			return nil, fmt.Errorf("scan category: %w", err)
		}
		categories = append(categories, cat)
	}
	return categories, rows.Err()
}

func (s *Storage) UpdateBankCategories(ctx context.Context, userID int64, monthStr, bankName string, newCategories []domain.CashbackCategory) error {
	if len(newCategories) == 0 {
		return fmt.Errorf("categories list cannot be empty")
	}
	for _, cc := range newCategories {
		if strings.TrimSpace(cc.Category.Name) == "" {
			return fmt.Errorf("category name cannot be empty")
		}
		if cc.Percent < 0 || cc.Percent > 100 {
			return fmt.Errorf("percent must be between 0 and 100 for category %q", cc.Category.Name)
		}
	}

	monthTime, err := time.Parse("2006-01", monthStr)
	if err != nil {
		return fmt.Errorf("invalid month: %w", err)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var monthID, bankID int
	err = tx.QueryRow(ctx, `
		SELECT cm.id, b.id
		FROM cashback_months cm
		JOIN bank_cashback_categories bcc ON bcc.cashback_month_id = cm.id
		JOIN banks b ON b.id = bcc.bank_id
		WHERE cm.user_id = $1 AND cm.month = $2 AND b.name = $3
		LIMIT 1
	`, userID, monthTime, bankName).Scan(&monthID, &bankID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return fmt.Errorf("bank %q not found in month %s", bankName, monthStr)
		}
		return fmt.Errorf("find bank in month: %w", err)
	}

	_, err = tx.Exec(ctx, `
		DELETE FROM bank_cashback_categories
		WHERE cashback_month_id = $1 AND bank_id = $2
	`, monthID, bankID)
	if err != nil {
		return fmt.Errorf("clear old categories: %w", err)
	}

	for _, cc := range newCategories {
		var catID int
		err = tx.QueryRow(ctx, `
			INSERT INTO categories (name) VALUES ($1)
			ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
			RETURNING id
		`, cc.Category.Name).Scan(&catID)
		if err != nil {
			return fmt.Errorf("create category %q: %w", cc.Category.Name, err)
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO bank_cashback_categories (cashback_month_id, bank_id, category_id, percent)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (cashback_month_id, bank_id, category_id) 
			DO UPDATE SET percent = EXCLUDED.percent
		`, monthID, bankID, catID, cc.Percent)
		if err != nil {
			return fmt.Errorf("link category: %w", err)
		}
	}

	return tx.Commit(ctx)
}

func (s *Storage) DeleteBankFromMonth(ctx context.Context, userID int64, monthStr, bankName string) error {
	monthTime, err := time.Parse("2006-01", monthStr)
	if err != nil {
		return fmt.Errorf("invalid month: %w", err)
	}

	bankName = sanitizeString(bankName)
	_, err = s.db.Exec(ctx, `
		DELETE FROM bank_cashback_categories
		USING banks b, cashback_months cm
		WHERE bank_cashback_categories.bank_id = b.id
		AND bank_cashback_categories.cashback_month_id = cm.id
		AND cm.user_id = $1
		AND cm.month = $2
		AND b.name = $3
	`, userID, monthTime, bankName)

	if err != nil {
		return fmt.Errorf("delete bank from month: %w", err)
	}
	return nil
}

func (s *Storage) DeleteCategoryFromBank(ctx context.Context, userID int64, monthStr, bankName, categoryName string) error {
	monthTime, err := time.Parse("2006-01", monthStr)
	log.Printf("🗑️ SQL: DELETE category='%s' from bank='%s'", categoryName, bankName)
	if err != nil {
		return fmt.Errorf("invalid month: %w", err)
	}

	bankName = sanitizeString(bankName)
	result, err := s.db.Exec(ctx, `
		DELETE FROM bank_cashback_categories
		USING banks b, categories c, cashback_months cm
		WHERE bank_cashback_categories.bank_id = b.id
		AND bank_cashback_categories.category_id = c.id
		AND bank_cashback_categories.cashback_month_id = cm.id
		AND cm.user_id = $1
		AND cm.month = $2
		AND b.name = $3
		AND c.name = $4
	`, userID, monthTime, bankName, categoryName)

	if err != nil {
		return fmt.Errorf("delete category from bank: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("category %q not found for bank %q in %s", categoryName, bankName, monthStr)
	}

	return nil
}

func (s *Storage) PatchMonth(ctx context.Context, userID int64, monthStr string, bankCategories []domain.BankWithCategories) error {
	for _, bc := range bankCategories {
		if strings.TrimSpace(bc.Bank.Name) == "" {
			return fmt.Errorf("bank name cannot be empty")
		}
		if len(bc.Categories) == 0 {
			return fmt.Errorf("bank %q must have at least one category", bc.Bank.Name)
		}
		for _, cc := range bc.Categories {
			if strings.TrimSpace(cc.Category.Name) == "" {
				return fmt.Errorf("category name cannot be empty for bank %q", bc.Bank.Name)
			}
			if cc.Percent < 0 || cc.Percent > 100 {
				return fmt.Errorf("percent must be between 0 and 100 for category %q", cc.Category.Name)
			}
		}
	}

	monthTime, err := time.Parse("2006-01", monthStr)
	if err != nil {
		return fmt.Errorf("invalid month format: %w", err)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var monthID int
	err = tx.QueryRow(ctx, `
		SELECT id FROM cashback_months WHERE user_id = $1 AND month = $2
	`, userID, monthTime).Scan(&monthID)
	if err != nil {
		if err == pgx.ErrNoRows {
			err = tx.QueryRow(ctx, `
				INSERT INTO cashback_months (user_id, month) VALUES ($1, $2) RETURNING id
			`, userID, monthTime).Scan(&monthID)
			if err != nil {
				return fmt.Errorf("create month: %w", err)
			}
		} else {
			return fmt.Errorf("check month: %w", err)
		}
	}

	createBankInTx := func(name string) (int, error) {
		var id int
		err := tx.QueryRow(ctx, `
			INSERT INTO banks (name) VALUES ($1)
			ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
			RETURNING id
		`, name).Scan(&id)
		return id, err
	}

	createCategoryInTx := func(name string) (int, error) {
		var id int
		err := tx.QueryRow(ctx, `
			INSERT INTO categories (name) VALUES ($1)
			ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
			RETURNING id
		`, name).Scan(&id)
		return id, err
	}

	for _, bc := range bankCategories {
		bankID, err := createBankInTx(bc.Bank.Name)
		if err != nil {
			return fmt.Errorf("create bank %q: %w", bc.Bank.Name, err)
		}

		for _, cc := range bc.Categories {
			categoryID, err := createCategoryInTx(cc.Category.Name)
			if err != nil {
				return fmt.Errorf("create category %q: %w", cc.Category.Name, err)
			}

			_, err = tx.Exec(ctx, `
				INSERT INTO bank_cashback_categories (cashback_month_id, bank_id, category_id, percent)
				VALUES ($1, $2, $3, $4)
				ON CONFLICT (cashback_month_id, bank_id, category_id) 
				DO UPDATE SET percent = EXCLUDED.percent
			`, monthID, bankID, categoryID, cc.Percent)
			if err != nil {
				return fmt.Errorf("upsert bank-category: %w", err)
			}
		}
	}

	return tx.Commit(ctx)
}