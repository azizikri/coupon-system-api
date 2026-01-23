package repository

import (
	"context"
	"fmt"

	db "github.com/azizikri/flash-sale-coupon/db/gen"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store interface {
	ExecTx(ctx context.Context, fn func(Querier) error) error
	CreateCoupon(ctx context.Context, arg db.CreateCouponParams) (db.Coupon, error)
	DecrementRemaining(ctx context.Context, name string) (db.Coupon, error)
	GetCouponByName(ctx context.Context, name string) (db.Coupon, error)
	InsertClaim(ctx context.Context, arg db.InsertClaimParams) (int64, error)
	ListClaimsByCoupon(ctx context.Context, couponName string) ([]string, error)
}

type Querier interface {
	InsertClaim(ctx context.Context, arg db.InsertClaimParams) (int64, error)
	DecrementRemaining(ctx context.Context, name string) (db.Coupon, error)
}

type store struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func New(pool *pgxpool.Pool) Store {
	return &store{
		pool:    pool,
		queries: db.New(pool),
	}
}

func (s *store) ExecTx(ctx context.Context, fn func(Querier) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	q := s.queries.WithTx(tx)
	if err := fn(q); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			return fmt.Errorf("tx err: %v, rollback err: %v", err, rbErr)
		}
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (s *store) CreateCoupon(ctx context.Context, arg db.CreateCouponParams) (db.Coupon, error) {
	return s.queries.CreateCoupon(ctx, arg)
}

func (s *store) DecrementRemaining(ctx context.Context, name string) (db.Coupon, error) {
	return s.queries.DecrementRemaining(ctx, name)
}

func (s *store) GetCouponByName(ctx context.Context, name string) (db.Coupon, error) {
	return s.queries.GetCouponByName(ctx, name)
}

func (s *store) InsertClaim(ctx context.Context, arg db.InsertClaimParams) (int64, error) {
	return s.queries.InsertClaim(ctx, arg)
}

func (s *store) ListClaimsByCoupon(ctx context.Context, couponName string) ([]string, error) {
	return s.queries.ListClaimsByCoupon(ctx, couponName)
}
