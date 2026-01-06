// internal/handler/cashback.go
package handler

import (
	"cashback-tracker/internal/domain"
	"cashback-tracker/internal/storage"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	val "cashback-tracker/internal/validator"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

type CombinedStorage interface {
	storage.CashbackStorage
	storage.BankStorage
	storage.CategoryStorage
}

type CashbackHandler struct {
	store CombinedStorage
}

func NewCashbackHandler(store CombinedStorage) *CashbackHandler {
	return &CashbackHandler{store: store}
}

// SaveMonth godoc
// @Summary Save cashback categories for a month
// @Description Save banks and their cashback categories for a given month
// @Tags cashback
// @Accept json
// @Produce json
// @Param request body SaveMonthRequest true "Month data"
// @Success 200 {object} map[string]string{"status":"ok"}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/v1/month [post]
func (h *CashbackHandler) SaveMonth(c *gin.Context) {
	slog.Info("SaveMonth request received")
	var req SaveMonthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}

	if err := validateStruct(req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userIDVal, ok := c.Get("user_id")
	if !ok {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "user_id missing"})
		return
	}
	userID, ok := userIDVal.(int64)
	if !ok {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "invalid user_id"})
		return
	}

	// Конвертируем запрос в доменную модель
	bankCategories := make([]domain.BankWithCategories, len(req.Banks))
	for i, bankReq := range req.Banks {
		categories := make([]domain.CashbackCategory, len(bankReq.Categories))
		for j, catReq := range bankReq.Categories {
			categories[j] = domain.CashbackCategory{
				Category: domain.Category{Name: catReq.Name},
				Percent:  catReq.Percent,
			}
		}
		bankCategories[i] = domain.BankWithCategories{
			Bank:       domain.Bank{Name: bankReq.Name},
			Categories: categories,
		}
	}

	if err := h.store.SaveMonth(context.Background(), userID, req.Month, bankCategories); err != nil {
		slog.Error("Failed to save month", "error", err, "user_id", userID, "month", req.Month)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save month"})
		return
	}

	slog.Info("Month saved successfully", "user_id", userID, "month", req.Month)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// GetMonth godoc
// @Summary Get cashback for a month
// @Param month query string true "Month in YYYY-MM format"
// @Success 200 {object} domain.CashbackMonth
// @Failure 400 {object} map[string]string
// @Router /api/v1/month [get]
func (h *CashbackHandler) GetMonth(c *gin.Context) {
	month := c.Query("month")
	if month == "" || len(month) != 7 || month[4] != '-' {
		c.JSON(http.StatusBadRequest, gin.H{"error": "month query param required in YYYY-MM format"})
		return
	}

	userIDVal, ok := c.Get("user_id")
	if !ok {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "user_id missing"})
		return
	}
	userID, ok := userIDVal.(int64)
	if !ok {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "invalid user_id"})
		return
	}

	result, err := h.store.GetMonth(context.Background(), userID, month)
	if err != nil {
		slog.Error("GetMonth failed", "error", err, "user_id", userID, "month", month)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal error"})
		return
	}
	if result == nil {
		c.JSON(http.StatusOK, domain.CashbackMonth{Month: month, Banks: []domain.BankWithCategories{}})
		return
	}
	c.JSON(http.StatusOK, result)
}

// SearchByCategory godoc
// @Summary Search banks by category name
// @Param month query string true "Month in YYYY-MM format"
// @Param q query string true "Category name"
// @Success 200 {array} domain.Bank
// @Failure 400 {object} map[string]string
// @Router /api/v1/search/category [get]
func (h *CashbackHandler) SearchByCategory(c *gin.Context) {
	month := c.Query("month")
	query := c.Query("q")
	if month == "" || query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "month and q query params required"})
		return
	}
	if len(month) != 7 || month[4] != '-' {
		c.JSON(http.StatusBadRequest, gin.H{"error": "month must be in YYYY-MM format"})
		return
	}

	userIDVal, ok := c.Get("user_id")
	if !ok {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "user_id missing"})
		return
	}
	userID, ok := userIDVal.(int64)
	if !ok {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "invalid user_id"})
		return
	}

	banks, err := h.store.SearchByCategory(context.Background(), userID, month, query)
	if err != nil {
		slog.Error("SearchByCategory failed", "error", err, "user_id", userID, "month", month, "category", query)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal error"})
		return
	}
	c.JSON(http.StatusOK, banks)
}

// SearchByBank godoc
// @Summary Search categories by bank name
// @Param month query string true "Month in YYYY-MM format"
// @Param q query string true "Bank name"
// @Success 200 {array} domain.Category
// @Failure 400 {object} map[string]string
// @Router /api/v1/search/bank [get]
func (h *CashbackHandler) SearchByBank(c *gin.Context) {
	month := c.Query("month")
	query := c.Query("q")
	if month == "" || query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "month and q query params required"})
		return
	}
	if len(month) != 7 || month[4] != '-' {
		c.JSON(http.StatusBadRequest, gin.H{"error": "month must be in YYYY-MM format"})
		return
	}

	userIDVal, ok := c.Get("user_id")
	if !ok {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "user_id missing"})
		return
	}
	userID, ok := userIDVal.(int64)
	if !ok {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "invalid user_id"})
		return
	}

	categories, err := h.store.SearchByBank(context.Background(), userID, month, query)
	if err != nil {
		slog.Error("SearchByBank failed", "error", err, "user_id", userID, "month", month, "bank", query)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal error"})
		return
	}
	c.JSON(http.StatusOK, categories)
}

// UpdateBankCategories godoc
// @Summary Update categories for a bank in a month
// @Param month query string true "Month in YYYY-MM format"
// @Param bank query string true "Bank name"
// @Param request body UpdateCategoriesRequest true "New categories"
// @Success 200 {object} map[string]string{"status":"ok"}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/v1/month/bank [patch]
func (h *CashbackHandler) UpdateBankCategories(c *gin.Context) {
	month := c.Query("month")
	bankName := c.Query("bank")
	if month == "" || bankName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "month and bank query params required"})
		return
	}

	var req UpdateCategoriesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}

	if err := validateStruct(req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userIDVal, ok := c.Get("user_id")
	if !ok {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "user_id missing"})
		return
	}
	userID, ok := userIDVal.(int64)
	if !ok {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "invalid user_id"})
		return
	}

	// Конвертируем в доменную модель
	categories := make([]domain.CashbackCategory, len(req.Categories))
	for i, catReq := range req.Categories {
		categories[i] = domain.CashbackCategory{
			Category: domain.Category{Name: catReq.Name},
			Percent:  catReq.Percent,
		}
	}

	if err := h.store.UpdateBankCategories(context.Background(), userID, month, bankName, categories); err != nil {
		slog.Error("UpdateBankCategories failed", "error", err, "user_id", userID, "month", month, "bank", bankName)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// PatchMonth godoc
// @Summary Partially update cashback for a month (add/update banks/categories)
// @Description Add or update banks and categories without removing others
// @Tags cashback
// @Accept json
// @Produce json
// @Param request body SaveMonthRequest true "Month data"
// @Success 200 {object} map[string]string{"status":"ok"}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/v1/month [patch]
func (h *CashbackHandler) PatchMonth(c *gin.Context) {
	slog.Info("PatchMonth request received")
	var req SaveMonthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}

	if err := validateStruct(req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userIDVal, ok := c.Get("user_id")
	if !ok {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "user_id missing"})
		return
	}
	userID, ok := userIDVal.(int64)
	if !ok {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "invalid user_id"})
		return
	}

	bankCategories := make([]domain.BankWithCategories, len(req.Banks))
	for i, bankReq := range req.Banks {
		categories := make([]domain.CashbackCategory, len(bankReq.Categories))
		for j, catReq := range bankReq.Categories {
			categories[j] = domain.CashbackCategory{
				Category: domain.Category{Name: catReq.Name},
				Percent:  catReq.Percent,
			}
		}
		bankCategories[i] = domain.BankWithCategories{
			Bank:       domain.Bank{Name: bankReq.Name},
			Categories: categories,
		}
	}

	if err := h.store.PatchMonth(context.Background(), userID, req.Month, bankCategories); err != nil {
		slog.Error("Failed to patch month", "error", err, "user_id", userID, "month", req.Month)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update month"})
		return
	}

	slog.Info("Month patched successfully", "user_id", userID, "month", req.Month)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// DeleteBankFromMonth godoc
// @Summary Delete a bank from a month
// @Param month query string true "Month in YYYY-MM format"
// @Param bank query string true "Bank name"
// @Success 200 {object} map[string]string{"status":"ok"}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/v1/month/bank [delete]
func (h *CashbackHandler) DeleteBankFromMonth(c *gin.Context) {
	month := c.Query("month")
	bankName := c.Query("bank")
	if month == "" || bankName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "month and bank query params required"})
		return
	}

	userIDVal, ok := c.Get("user_id")
	if !ok {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "user_id missing"})
		return
	}
	userID, ok := userIDVal.(int64)
	if !ok {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "invalid user_id"})
		return
	}

	if err := h.store.DeleteBankFromMonth(context.Background(), userID, month, bankName); err != nil {
		slog.Error("DeleteBankFromMonth failed", "error", err, "user_id", userID, "month", month, "bank", bankName)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// DeleteCategoryFromBank godoc
// @Summary Delete a category from a bank in a month
// @Param month query string true "Month in YYYY-MM format"
// @Param bank query string true "Bank name"
// @Param category query string true "Category name"
// @Success 200 {object} map[string]string{"status":"ok"}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/v1/month/bank/category [delete]
func (h *CashbackHandler) DeleteCategoryFromBank(c *gin.Context) {
	month := c.Query("month")
	bankName := c.Query("bank")
	categoryName := c.Query("category")
	if month == "" || bankName == "" || categoryName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "month, bank, and category query params required"})
		return
	}

	userIDVal, ok := c.Get("user_id")
	if !ok {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "user_id missing"})
		return
	}
	userID, ok := userIDVal.(int64)
	if !ok {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "invalid user_id"})
		return
	}

	if err := h.store.DeleteCategoryFromBank(context.Background(), userID, month, bankName, categoryName); err != nil {
		slog.Error("DeleteCategoryFromBank failed", "error", err, "user_id", userID, "month", month, "bank", bankName, "category", categoryName)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Нет подходящей категории"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// === DTO ===

type SaveMonthRequest struct {
	Month string `json:"month" validate:"required,yearmonth"`
	Banks []struct {
		Name       string `json:"name" validate:"required,notblank"`
		Categories []struct {
			Name    string  `json:"name" validate:"required,notblank"`
			Percent float32 `json:"percent" validate:"required,gte=0,lte=100"`
		} `json:"categories" validate:"required,min=1,dive"`
	} `json:"banks" validate:"required,min=1"`
}

type UpdateCategoriesRequest struct {
	Categories []struct {
		Name    string  `json:"name" validate:"required,notblank"`
		Percent float32 `json:"percent" validate:"required,gte=0,lte=100"`
	} `json:"categories" validate:"required,min=1,dive"`
}

func validateStruct(v any) error {
	if err := val.Validate.Struct(v); err != nil {
		var errs []string
		for _, e := range err.(validator.ValidationErrors) {
			errs = append(errs, fieldErrorToString(e))
		}
		return fmt.Errorf("invalid input: %s", strings.Join(errs, "; "))
	}
	return nil
}

func fieldErrorToString(e validator.FieldError) string {
	switch e.Tag() {
	case "required":
		return fmt.Sprintf("%s is required", e.Field())
	case "yearmonth":
		return fmt.Sprintf("%s must be in YYYY-MM format", e.Field())
	case "notblank":
		return fmt.Sprintf("%s must not be blank", e.Field())
	case "min":
		if e.Param() == "1" {
			return fmt.Sprintf("%s must not be empty", e.Field())
		}
		return fmt.Sprintf("%s is too short", e.Field())
	case "gte", "lte":
		return fmt.Sprintf("%s must be between 0 and 100", e.Field())
	default:
		return fmt.Sprintf("%s is invalid", e.Field())
	}
}