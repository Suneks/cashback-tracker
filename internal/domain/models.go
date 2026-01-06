// internal/domain/models.go
package domain

type Bank struct {
	ID   int    `json:"-"`
	Name string `json:"name"`
}

type Category struct {
	ID   int    `json:"-"`
	Name string `json:"name"`
}

// CashbackCategory — категория + процент
type CashbackCategory struct {
	Category Category `json:"category"`
	Percent  float32  `json:"percent"`
}

type BankWithCategories struct {
	Bank      Bank              `json:"bank"`
	Categories []CashbackCategory `json:"categories"`
}

type CashbackMonth struct {
	Month  string               `json:"month"`
	UserID int64                  `json:"-"`
	Banks  []BankWithCategories `json:"banks"`
}