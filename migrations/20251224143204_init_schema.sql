-- +goose Up
-- +goose StatementBegin
CREATE TABLE banks (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE categories (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE cashback_months (
    id SERIAL PRIMARY KEY,
    month DATE NOT NULL,          -- '2024-12-01'
    user_id INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE bank_cashback_categories (
    id SERIAL PRIMARY KEY,
    cashback_month_id INTEGER NOT NULL REFERENCES cashback_months(id) ON DELETE CASCADE,
    bank_id INTEGER NOT NULL REFERENCES banks(id) ON DELETE CASCADE,
    category_id INTEGER NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    UNIQUE(cashback_month_id, bank_id, category_id)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS bank_cashback_categories;
DROP TABLE IF EXISTS cashback_months;
DROP TABLE IF EXISTS categories;
DROP TABLE IF EXISTS banks;
-- +goose StatementEnd