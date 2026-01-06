-- +goose Up
-- +goose StatementBegin
ALTER TABLE cashback_months 
ALTER COLUMN user_id TYPE BIGINT USING user_id::BIGINT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Откат: только если user_id <= 2147483647
ALTER TABLE cashback_months 
ALTER COLUMN user_id TYPE INT USING user_id::INT;
-- +goose StatementEnd