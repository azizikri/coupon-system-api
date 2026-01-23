package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/azizikri/flash-sale-coupon/internal/repository"
	"github.com/azizikri/flash-sale-coupon/internal/usecase"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	testPool   *pgxpool.Pool
	testServer *httptest.Server
)

const testDSN = "postgresql://test:test@localhost:5433/coupondb_test?sslmode=disable"

func TestMain(m *testing.M) {
	if err := startPostgres(); err != nil {
		fmt.Printf("Failed to start postgres: %v\n", err)
		os.Exit(1)
	}

	if err := waitForPostgres(); err != nil {
		fmt.Printf("Postgres not ready: %v\n", err)
		stopPostgres()
		os.Exit(1)
	}

	if err := runMigrations(); err != nil {
		fmt.Printf("Failed to run migrations: %v\n", err)
		stopPostgres()
		os.Exit(1)
	}

	var err error
	testPool, err = pgxpool.New(context.Background(), testDSN)
	if err != nil {
		fmt.Printf("Failed to create pool: %v\n", err)
		stopPostgres()
		os.Exit(1)
	}

	store := repository.New(testPool)
	service := usecase.NewCouponService(store)
	handler := NewHandler(service)

	r := chi.NewRouter()
	handler.Routes(r)
	testServer = httptest.NewServer(r)

	code := m.Run()

	testServer.Close()
	testPool.Close()
	stopPostgres()

	os.Exit(code)
}

func startPostgres() error {
	cmd := exec.Command("docker-compose", "-f", "docker-compose.test.yml", "up", "-d")
	cmd.Dir = "../../../" // Go to project root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func stopPostgres() {
	cmd := exec.Command("docker-compose", "-f", "docker-compose.test.yml", "down", "-v")
	cmd.Dir = "../../../"
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()
}

func waitForPostgres() error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for postgres")
		default:
			pool, err := pgxpool.New(context.Background(), testDSN)
			if err == nil {
				if err := pool.Ping(context.Background()); err == nil {
					pool.Close()
					return nil
				}
				pool.Close()
			}
			time.Sleep(1 * time.Second)
		}
	}
}

func runMigrations() error {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, testDSN)
	if err != nil {
		return err
	}
	defer pool.Close()

	migration := `
CREATE TABLE IF NOT EXISTS coupons (
    name TEXT PRIMARY KEY,
    amount INTEGER NOT NULL CHECK (amount >= 0),
    remaining_amount INTEGER NOT NULL CHECK (remaining_amount >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_coupons_remaining_amount ON coupons(remaining_amount);

CREATE TABLE IF NOT EXISTS claims (
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

CREATE INDEX IF NOT EXISTS idx_claims_coupon_name ON claims(coupon_name, created_at);
CREATE INDEX IF NOT EXISTS idx_claims_user_id ON claims(user_id);
`
	_, err = pool.Exec(ctx, migration)
	return err
}

func cleanupDB(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	_, err := testPool.Exec(ctx, "DELETE FROM claims")
	if err != nil {
		t.Fatalf("failed to cleanup claims: %v", err)
	}
	_, err = testPool.Exec(ctx, "DELETE FROM coupons")
	if err != nil {
		t.Fatalf("failed to cleanup coupons: %v", err)
	}
}

func TestCreateCoupon_Success(t *testing.T) {
	cleanupDB(t)

	body := `{"name": "test-coupon", "amount": 100}`
	resp, err := http.Post(testServer.URL+"/api/coupons", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected status 201, got %d", resp.StatusCode)
	}
}

func TestCreateCoupon_Duplicate(t *testing.T) {
	cleanupDB(t)

	body := `{"name": "dup-coupon", "amount": 100}`

	resp, err := http.Post(testServer.URL+"/api/coupons", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("first create should succeed, got %d", resp.StatusCode)
	}

	resp, err = http.Post(testServer.URL+"/api/coupons", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Errorf("expected status 409 for duplicate, got %d", resp.StatusCode)
	}
}

func TestClaimCoupon_Success(t *testing.T) {
	cleanupDB(t)

	createBody := `{"name": "claim-test", "amount": 10}`
	resp, _ := http.Post(testServer.URL+"/api/coupons", "application/json", bytes.NewBufferString(createBody))
	resp.Body.Close()

	claimBody := `{"user_id": "user1", "coupon_name": "claim-test"}`
	resp, err := http.Post(testServer.URL+"/api/coupons/claim", "application/json", bytes.NewBufferString(claimBody))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestClaimCoupon_AlreadyClaimed(t *testing.T) {
	cleanupDB(t)

	createBody := `{"name": "already-claimed-test", "amount": 10}`
	resp, _ := http.Post(testServer.URL+"/api/coupons", "application/json", bytes.NewBufferString(createBody))
	resp.Body.Close()

	claimBody := `{"user_id": "user1", "coupon_name": "already-claimed-test"}`
	resp, _ = http.Post(testServer.URL+"/api/coupons/claim", "application/json", bytes.NewBufferString(claimBody))
	resp.Body.Close()

	resp, err := http.Post(testServer.URL+"/api/coupons/claim", "application/json", bytes.NewBufferString(claimBody))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Errorf("expected status 409 for already claimed, got %d", resp.StatusCode)
	}
}

func TestClaimCoupon_NotFound(t *testing.T) {
	cleanupDB(t)

	claimBody := `{"user_id": "user1", "coupon_name": "nonexistent-coupon"}`
	resp, err := http.Post(testServer.URL+"/api/coupons/claim", "application/json", bytes.NewBufferString(claimBody))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404 for not found, got %d", resp.StatusCode)
	}
}

func TestClaimCoupon_SoldOut(t *testing.T) {
	cleanupDB(t)

	createBody := `{"name": "soldout-test", "amount": 1}`
	resp, _ := http.Post(testServer.URL+"/api/coupons", "application/json", bytes.NewBufferString(createBody))
	resp.Body.Close()

	claimBody := `{"user_id": "user1", "coupon_name": "soldout-test"}`
	resp, _ = http.Post(testServer.URL+"/api/coupons/claim", "application/json", bytes.NewBufferString(claimBody))
	resp.Body.Close()

	claimBody = `{"user_id": "user2", "coupon_name": "soldout-test"}`
	resp, err := http.Post(testServer.URL+"/api/coupons/claim", "application/json", bytes.NewBufferString(claimBody))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Errorf("expected status 409 for sold out, got %d", resp.StatusCode)
	}
}

func TestGetCouponDetails_Success(t *testing.T) {
	cleanupDB(t)

	createBody := `{"name": "get-test", "amount": 100}`
	resp, _ := http.Post(testServer.URL+"/api/coupons", "application/json", bytes.NewBufferString(createBody))
	resp.Body.Close()

	claimBody := `{"user_id": "user1", "coupon_name": "get-test"}`
	resp, _ = http.Post(testServer.URL+"/api/coupons/claim", "application/json", bytes.NewBufferString(claimBody))
	resp.Body.Close()

	resp, err := http.Get(testServer.URL + "/api/coupons/get-test")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var coupon CouponResponse
	if err := json.NewDecoder(resp.Body).Decode(&coupon); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if coupon.Name != "get-test" {
		t.Errorf("expected name 'get-test', got '%s'", coupon.Name)
	}
	if coupon.Amount != 100 {
		t.Errorf("expected amount 100, got %d", coupon.Amount)
	}
	if coupon.RemainingAmount != 99 {
		t.Errorf("expected remaining_amount 99, got %d", coupon.RemainingAmount)
	}
	if len(coupon.ClaimedBy) != 1 || coupon.ClaimedBy[0] != "user1" {
		t.Errorf("expected claimed_by ['user1'], got %v", coupon.ClaimedBy)
	}
}

func TestGetCouponDetails_NotFound(t *testing.T) {
	cleanupDB(t)

	resp, err := http.Get(testServer.URL + "/api/coupons/nonexistent")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", resp.StatusCode)
	}
}

func TestConcurrency_50ClaimsFor5Stock(t *testing.T) {
	cleanupDB(t)

	createBody := `{"name": "concurrent-5", "amount": 5}`
	resp, _ := http.Post(testServer.URL+"/api/coupons", "application/json", bytes.NewBufferString(createBody))
	resp.Body.Close()

	var (
		wg            sync.WaitGroup
		successCount  int32
		conflictCount int32
	)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(userID int) {
			defer wg.Done()

			claimBody := fmt.Sprintf(`{"user_id": "user%d", "coupon_name": "concurrent-5"}`, userID)
			resp, err := http.Post(testServer.URL+"/api/coupons/claim", "application/json", bytes.NewBufferString(claimBody))
			if err != nil {
				t.Errorf("request failed: %v", err)
				return
			}
			defer resp.Body.Close()

			switch resp.StatusCode {
			case http.StatusOK:
				atomic.AddInt32(&successCount, 1)
			case http.StatusConflict:
				atomic.AddInt32(&conflictCount, 1)
			default:
				t.Errorf("unexpected status code: %d", resp.StatusCode)
			}
		}(i)
	}

	wg.Wait()

	if successCount != 5 {
		t.Errorf("expected exactly 5 successful claims, got %d", successCount)
	}
	if conflictCount != 45 {
		t.Errorf("expected exactly 45 conflict responses, got %d", conflictCount)
	}

	resp, err := http.Get(testServer.URL + "/api/coupons/concurrent-5")
	if err != nil {
		t.Fatalf("failed to get coupon: %v", err)
	}
	defer resp.Body.Close()

	var coupon CouponResponse
	json.NewDecoder(resp.Body).Decode(&coupon)

	if coupon.RemainingAmount != 0 {
		t.Errorf("expected remaining_amount 0, got %d", coupon.RemainingAmount)
	}
	if len(coupon.ClaimedBy) != 5 {
		t.Errorf("expected 5 users in claimed_by, got %d", len(coupon.ClaimedBy))
	}
}

func TestConcurrency_10SameUserClaims(t *testing.T) {
	cleanupDB(t)

	createBody := `{"name": "same-user-test", "amount": 100}`
	resp, _ := http.Post(testServer.URL+"/api/coupons", "application/json", bytes.NewBufferString(createBody))
	resp.Body.Close()

	var (
		wg            sync.WaitGroup
		successCount  int32
		conflictCount int32
	)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			claimBody := `{"user_id": "same-user", "coupon_name": "same-user-test"}`
			resp, err := http.Post(testServer.URL+"/api/coupons/claim", "application/json", bytes.NewBufferString(claimBody))
			if err != nil {
				t.Errorf("request failed: %v", err)
				return
			}
			defer resp.Body.Close()

			switch resp.StatusCode {
			case http.StatusOK:
				atomic.AddInt32(&successCount, 1)
			case http.StatusConflict:
				atomic.AddInt32(&conflictCount, 1)
			default:
				t.Errorf("unexpected status code: %d", resp.StatusCode)
			}
		}()
	}

	wg.Wait()

	if successCount != 1 {
		t.Errorf("expected exactly 1 successful claim, got %d", successCount)
	}
	if conflictCount != 9 {
		t.Errorf("expected exactly 9 conflict responses, got %d", conflictCount)
	}

	resp, err := http.Get(testServer.URL + "/api/coupons/same-user-test")
	if err != nil {
		t.Fatalf("failed to get coupon: %v", err)
	}
	defer resp.Body.Close()

	var coupon CouponResponse
	json.NewDecoder(resp.Body).Decode(&coupon)

	if coupon.RemainingAmount != 99 {
		t.Errorf("expected remaining_amount 99, got %d", coupon.RemainingAmount)
	}
	if len(coupon.ClaimedBy) != 1 {
		t.Errorf("expected 1 user in claimed_by, got %d", len(coupon.ClaimedBy))
	}
	if len(coupon.ClaimedBy) > 0 && coupon.ClaimedBy[0] != "same-user" {
		t.Errorf("expected claimed_by ['same-user'], got %v", coupon.ClaimedBy)
	}
}

func TestConcurrency_MixedScenario(t *testing.T) {
	cleanupDB(t)

	coupons := []struct {
		name   string
		amount int
	}{
		{"mixed-coupon-1", 3},
		{"mixed-coupon-2", 5},
		{"mixed-coupon-3", 2},
	}

	for _, c := range coupons {
		body := fmt.Sprintf(`{"name": "%s", "amount": %d}`, c.name, c.amount)
		resp, _ := http.Post(testServer.URL+"/api/coupons", "application/json", bytes.NewBufferString(body))
		resp.Body.Close()
	}

	var wg sync.WaitGroup
	results := make(map[string]*struct {
		success  int32
		conflict int32
	})
	var mu sync.Mutex

	for _, c := range coupons {
		mu.Lock()
		results[c.name] = &struct {
			success  int32
			conflict int32
		}{}
		mu.Unlock()
	}

	for userID := 0; userID < 30; userID++ {
		for _, c := range coupons {
			wg.Add(1)
			go func(uid int, couponName string) {
				defer wg.Done()

				claimBody := fmt.Sprintf(`{"user_id": "user%d", "coupon_name": "%s"}`, uid, couponName)
				resp, err := http.Post(testServer.URL+"/api/coupons/claim", "application/json", bytes.NewBufferString(claimBody))
				if err != nil {
					return
				}
				defer resp.Body.Close()

				mu.Lock()
				defer mu.Unlock()
				switch resp.StatusCode {
				case http.StatusOK:
					results[couponName].success++
				case http.StatusConflict:
					results[couponName].conflict++
				}
			}(userID, c.name)
		}
	}

	wg.Wait()

	for _, c := range coupons {
		r := results[c.name]
		if int(r.success) != c.amount {
			t.Errorf("coupon %s: expected %d successful claims, got %d", c.name, c.amount, r.success)
		}
		expectedConflicts := 30 - c.amount
		if int(r.conflict) != expectedConflicts {
			t.Errorf("coupon %s: expected %d conflicts, got %d", c.name, expectedConflicts, r.conflict)
		}

		resp, _ := http.Get(testServer.URL + "/api/coupons/" + c.name)
		var coupon CouponResponse
		json.NewDecoder(resp.Body).Decode(&coupon)
		resp.Body.Close()

		if coupon.RemainingAmount != 0 {
			t.Errorf("coupon %s: expected remaining_amount 0, got %d", c.name, coupon.RemainingAmount)
		}
	}
}
