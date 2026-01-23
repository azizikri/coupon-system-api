package usecase

import (
	"context"

	"github.com/azizikri/flash-sale-coupon/internal/domain"
)

type CouponGateway interface {
	CreateCoupon(ctx context.Context, name string, amount int) error
	ClaimCoupon(ctx context.Context, userID, couponName string) error
	GetCouponDetails(ctx context.Context, name string) (*domain.Coupon, error)
}
