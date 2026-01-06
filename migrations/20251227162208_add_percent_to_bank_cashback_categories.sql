-- +goose Up
-- +goose StatementBegin
ALTER TABLE bank_cashback_categories 
ADD COLUMN percent NUMERIC(5,2) NOT NULL DEFAULT 1.00;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE bank_cashback_categories 
DROP COLUMN percent;
-- +goose StatementEnd