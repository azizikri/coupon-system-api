package usecase

import (
	"context"
	"errors"
	"strings"

	db "github.com/azizikri/flash-sale-coupon/db/gen"
	"github.com/azizikri/flash-sale-coupon/internal/domain"
	"github.com/azizikri/flash-sale-coupon/internal/repository"
	"github.com/jackc/pgx/v5"
)

type CouponService struct {
	store repository.Store
}

func NewCouponService(store repository.Store) *CouponService {
	return &CouponService{store: store}
}

func (s *CouponService) CreateCoupon(ctx context.Context, name string, amount int) error {
	_, err := s.store.CreateCoupon(ctx, db.CreateCouponParams{
		Name:            name,
		Amount:          int32(amount),
		RemainingAmount: int32(amount),
	})
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			return domain.ErrDuplicateCoupon
		}
		return err
	}
	return nil
}

func (s *CouponService) ClaimCoupon(ctx context.Context, userID, couponName string) error {
	return s.store.ExecTx(ctx, func(q repository.Querier) error {
		rowsAffected, err := q.InsertClaim(ctx, db.InsertClaimParams{
			UserID:     userID,
			CouponName: couponName,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) || strings.Contains(err.Error(), "foreign key") {
				return domain.ErrNotFound
			}
			return err
		}
		if rowsAffected == 0 {
			return domain.ErrAlreadyClaimed
		}

		_, err = q.DecrementRemaining(ctx, couponName)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.ErrSoldOut
			}
			return err
		}

		return nil
	})
}

func (s *CouponService) GetCouponDetails(ctx context.Context, name string) (*domain.Coupon, error) {
	coupon, err := s.store.GetCouponByName(ctx, name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}

	claimedBy, err := s.store.ListClaimsByCoupon(ctx, name)
	if err != nil {
		return nil, err
	}

	return &domain.Coupon{
		Name:            coupon.Name,
		Amount:          int(coupon.Amount),
		RemainingAmount: int(coupon.RemainingAmount),
		ClaimedBy:       claimedBy,
	}, nil
}
