-- +goose Up
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS pg_trgm;
-- +goose StatementEnd

-- +goose StatementBegin
-- GiST, not GIN: the precedent lookup is a KNN ordering (title <-> $1), which
-- only GiST supports.
CREATE INDEX releases_decided_title_trgm_idx ON releases USING gist (title gist_trgm_ops)
    WHERE match_status IN ('matched','suppressed');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- The extension stays: other objects in the database may depend on it, and it
-- is a declared prerequisite rather than something this migration owns.
DROP INDEX IF EXISTS releases_decided_title_trgm_idx;
-- +goose StatementEnd
