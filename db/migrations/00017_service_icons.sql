-- +goose Up

ALTER TABLE services ADD COLUMN icon_svg TEXT NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE services DROP COLUMN icon_svg;
