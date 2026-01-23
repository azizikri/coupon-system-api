package domain

import "errors"

var (
	ErrAlreadyClaimed  = errors.New("user has already claimed this coupon")
	ErrSoldOut         = errors.New("coupon is sold out")
	ErrNotFound        = errors.New("coupon not found")
	ErrDuplicateCoupon = errors.New("coupon already exists")
)

type Coupon struct {
	Name            string
	Amount          int
	RemainingAmount int
	ClaimedBy       []string
}

type Claim struct {
	UserID     string
	CouponName string
}
