-- name: CreateCoupon :one
INSERT INTO coupons (name, amount, remaining_amount, created_at)
VALUES ($1, $2, $3, NOW())
RETURNING *;

-- name: GetCouponByName :one
SELECT * FROM coupons
WHERE name = $1;

-- name: DecrementRemaining :one
UPDATE coupons
SET remaining_amount = remaining_amount - 1
WHERE name = $1 AND remaining_amount > 0
RETURNING *;

-- name: InsertClaim :execrows
INSERT INTO claims (user_id, coupon_name, created_at)
VALUES ($1, $2, NOW())
ON CONFLICT (user_id, coupon_name) DO NOTHING;

-- name: ListClaimsByCoupon :many
SELECT user_id FROM claims
WHERE coupon_name = $1
ORDER BY created_at ASC;
