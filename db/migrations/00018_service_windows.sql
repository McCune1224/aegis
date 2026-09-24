-- +goose Up

ALTER TABLE focus_windows RENAME TO service_windows;
ALTER TABLE service_windows ADD COLUMN action TEXT NOT NULL DEFAULT 'block';

-- +goose Down

ALTER TABLE service_windows DROP COLUMN action;
ALTER TABLE service_windows RENAME TO focus_windows;
