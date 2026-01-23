-- +goose Up
-- +goose StatementBegin

CREATE TABLE coupons (
    name TEXT PRIMARY KEY,
    amount INTEGER NOT NULL CHECK (amount >= 0),
    remaining_amount INTEGER NOT NULL CHECK (remaining_amount >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_coupons_remaining_amount ON coupons(remaining_amount);

CREATE TABLE claims (
    id SERIAL PRIMARY KEY,
    user_id TEXT NOT NULL,
    coupon_name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    CONSTRAINT fk_claims_coupon 
        FOREIGN KEY (coupon_name) 
        REFERENCES coupons(name) 
        ON DELETE RESTRICT,
    
    CONSTRAINT uq_user_coupon 
        UNIQUE (user_id, coupon_name)
);

CREATE INDEX idx_claims_coupon_name ON claims(coupon_name, created_at);
CREATE INDEX idx_claims_user_id ON claims(user_id);

-- +goose StatementEnd
