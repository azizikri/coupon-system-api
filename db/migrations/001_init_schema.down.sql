-- +goose Down
-- +goose StatementBegin

-- Drop tables in reverse order to respect foreign key constraints
-- Drop claims table first since it references coupons
DROP TABLE IF EXISTS claims;

-- Drop coupons table
DROP TABLE IF EXISTS coupons;

-- +goose StatementEnd
