-- +goose NO TRANSACTION
-- +goose Up
-- +goose StatementBegin
ALTER TYPE match_status ADD VALUE IF NOT EXISTS 'dead';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Postgres cannot drop a value from an enum. Reverting 'dead' would mean
-- rewriting the type and every column that uses it, which is far more
-- destructive than leaving an unused label in place.
SELECT 1;
-- +goose StatementEnd
