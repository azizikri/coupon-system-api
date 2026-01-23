package usecase

import (
	"context"
	"errors"
	"testing"

	db "github.com/azizikri/flash-sale-coupon/db/gen"
	"github.com/azizikri/flash-sale-coupon/internal/domain"
	"github.com/azizikri/flash-sale-coupon/internal/repository"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type mockStore struct {
	createCouponFn       func(ctx context.Context, arg db.CreateCouponParams) (db.Coupon, error)
	getCouponByNameFn    func(ctx context.Context, name string) (db.Coupon, error)
	decrementRemainingFn func(ctx context.Context, name string) (db.Coupon, error)
	insertClaimFn        func(ctx context.Context, arg db.InsertClaimParams) (int64, error)
	listClaimsByCouponFn func(ctx context.Context, couponName string) ([]string, error)
	execTxFn             func(ctx context.Context, fn func(repository.Querier) error) error
}

func (m *mockStore) CreateCoupon(ctx context.Context, arg db.CreateCouponParams) (db.Coupon, error) {
	if m.createCouponFn != nil {
		return m.createCouponFn(ctx, arg)
	}
	return db.Coupon{}, nil
}

func (m *mockStore) GetCouponByName(ctx context.Context, name string) (db.Coupon, error) {
	if m.getCouponByNameFn != nil {
		return m.getCouponByNameFn(ctx, name)
	}
	return db.Coupon{}, nil
}

func (m *mockStore) DecrementRemaining(ctx context.Context, name string) (db.Coupon, error) {
	if m.decrementRemainingFn != nil {
		return m.decrementRemainingFn(ctx, name)
	}
	return db.Coupon{}, nil
}

func (m *mockStore) InsertClaim(ctx context.Context, arg db.InsertClaimParams) (int64, error) {
	if m.insertClaimFn != nil {
		return m.insertClaimFn(ctx, arg)
	}
	return 1, nil
}

func (m *mockStore) ListClaimsByCoupon(ctx context.Context, couponName string) ([]string, error) {
	if m.listClaimsByCouponFn != nil {
		return m.listClaimsByCouponFn(ctx, couponName)
	}
	return nil, nil
}

func (m *mockStore) ExecTx(ctx context.Context, fn func(repository.Querier) error) error {
	if m.execTxFn != nil {
		return m.execTxFn(ctx, fn)
	}
	return fn(m)
}

func TestCreateCoupon_Success(t *testing.T) {
	store := &mockStore{
		createCouponFn: func(ctx context.Context, arg db.CreateCouponParams) (db.Coupon, error) {
			return db.Coupon{
				Name:            arg.Name,
				Amount:          arg.Amount,
				RemainingAmount: arg.RemainingAmount,
			}, nil
		},
	}

	svc := NewCouponService(store)
	err := svc.CreateCoupon(context.Background(), "test-coupon", 100)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestCreateCoupon_Duplicate(t *testing.T) {
	store := &mockStore{
		createCouponFn: func(ctx context.Context, arg db.CreateCouponParams) (db.Coupon, error) {
			return db.Coupon{}, errors.New("duplicate key value violates unique constraint")
		},
	}

	svc := NewCouponService(store)
	err := svc.CreateCoupon(context.Background(), "test-coupon", 100)
	if !errors.Is(err, domain.ErrDuplicateCoupon) {
		t.Fatalf("expected ErrDuplicateCoupon, got %v", err)
	}
}

func TestClaimCoupon_Success(t *testing.T) {
	store := &mockStore{
		insertClaimFn: func(ctx context.Context, arg db.InsertClaimParams) (int64, error) {
			return 1, nil
		},
		decrementRemainingFn: func(ctx context.Context, name string) (db.Coupon, error) {
			return db.Coupon{Name: name, RemainingAmount: 99}, nil
		},
	}

	svc := NewCouponService(store)
	err := svc.ClaimCoupon(context.Background(), "user1", "test-coupon")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestClaimCoupon_AlreadyClaimed(t *testing.T) {
	store := &mockStore{
		insertClaimFn: func(ctx context.Context, arg db.InsertClaimParams) (int64, error) {
			return 0, nil
		},
	}

	svc := NewCouponService(store)
	err := svc.ClaimCoupon(context.Background(), "user1", "test-coupon")
	if !errors.Is(err, domain.ErrAlreadyClaimed) {
		t.Fatalf("expected ErrAlreadyClaimed, got %v", err)
	}
}

func TestClaimCoupon_SoldOut(t *testing.T) {
	store := &mockStore{
		insertClaimFn: func(ctx context.Context, arg db.InsertClaimParams) (int64, error) {
			return 1, nil
		},
		decrementRemainingFn: func(ctx context.Context, name string) (db.Coupon, error) {
			return db.Coupon{}, pgx.ErrNoRows
		},
	}

	svc := NewCouponService(store)
	err := svc.ClaimCoupon(context.Background(), "user1", "test-coupon")
	if !errors.Is(err, domain.ErrSoldOut) {
		t.Fatalf("expected ErrSoldOut, got %v", err)
	}
}

func TestClaimCoupon_NotFound(t *testing.T) {
	store := &mockStore{
		insertClaimFn: func(ctx context.Context, arg db.InsertClaimParams) (int64, error) {
			return 0, errors.New("foreign key constraint violation")
		},
	}

	svc := NewCouponService(store)
	err := svc.ClaimCoupon(context.Background(), "user1", "nonexistent-coupon")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetCouponDetails_Success(t *testing.T) {
	store := &mockStore{
		getCouponByNameFn: func(ctx context.Context, name string) (db.Coupon, error) {
			return db.Coupon{
				Name:            name,
				Amount:          100,
				RemainingAmount: 50,
				CreatedAt:       pgtype.Timestamptz{},
			}, nil
		},
		listClaimsByCouponFn: func(ctx context.Context, couponName string) ([]string, error) {
			return []string{"user1", "user2"}, nil
		},
	}

	svc := NewCouponService(store)
	coupon, err := svc.GetCouponDetails(context.Background(), "test-coupon")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if coupon.Name != "test-coupon" {
		t.Fatalf("expected name test-coupon, got %s", coupon.Name)
	}
	if coupon.Amount != 100 {
		t.Fatalf("expected amount 100, got %d", coupon.Amount)
	}
	if coupon.RemainingAmount != 50 {
		t.Fatalf("expected remaining 50, got %d", coupon.RemainingAmount)
	}
	if len(coupon.ClaimedBy) != 2 {
		t.Fatalf("expected 2 claims, got %d", len(coupon.ClaimedBy))
	}
}

func TestGetCouponDetails_NotFound(t *testing.T) {
	store := &mockStore{
		getCouponByNameFn: func(ctx context.Context, name string) (db.Coupon, error) {
			return db.Coupon{}, pgx.ErrNoRows
		},
	}

	svc := NewCouponService(store)
	_, err := svc.GetCouponDetails(context.Background(), "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
