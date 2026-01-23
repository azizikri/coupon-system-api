package kafka

import (
	"context"

	"github.com/azizikri/flash-sale-coupon/internal/domain"
	"github.com/azizikri/flash-sale-coupon/internal/usecase"
)

type DirectGateway struct {
	service *usecase.CouponService
}

func NewDirectGateway(service *usecase.CouponService) usecase.CouponGateway {
	return &DirectGateway{service: service}
}

func (g *DirectGateway) CreateCoupon(ctx context.Context, name string, amount int) error {
	return g.service.CreateCoupon(ctx, name, amount)
}

func (g *DirectGateway) ClaimCoupon(ctx context.Context, userID, couponName string) error {
	return g.service.ClaimCoupon(ctx, userID, couponName)
}

func (g *DirectGateway) GetCouponDetails(ctx context.Context, name string) (*domain.Coupon, error) {
	return g.service.GetCouponDetails(ctx, name)
}
