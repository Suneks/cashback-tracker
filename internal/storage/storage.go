// internal/storage/storage.go
package storage

import (
	"cashback-tracker/internal/domain"
	"context"
)

type BankStorage interface {
	CreateIfNotExists(ctx context.Context, name string) (int, error)
	FindByName(ctx context.Context, name string) (*domain.Bank, error)
}

type CategoryStorage interface {
	CreateCategoryIfNotExists(ctx context.Context, name string) (int, error)
	FindCategoryByName(ctx context.Context, name string) (*domain.Category, error)
}

type CashbackStorage interface {
	SaveMonth(ctx context.Context, userID int64, monthTime string, bankCategories []domain.BankWithCategories) error
	GetMonth(ctx context.Context, userID int64, monthTime string) (*domain.CashbackMonth, error)
	SearchByCategory(ctx context.Context, userID int64, monthTime string, categoryName string) ([]domain.Bank, error)
	SearchByBank(ctx context.Context, userID int64, monthTime string, bankName string) ([]domain.Category, error)
	UpdateBankCategories(ctx context.Context, userID int64, monthTime string, bankName string, categories []domain.CashbackCategory) error
	PatchMonth(ctx context.Context, userID int64, monthTime string, bankCategories []domain.BankWithCategories) error
	DeleteBankFromMonth(ctx context.Context, userID int64, monthTime string, bankName string) error
	DeleteCategoryFromBank(ctx context.Context, userID int64, monthTime string, bankName string, categoryName string) error
}
