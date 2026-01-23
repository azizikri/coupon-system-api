package http

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/azizikri/flash-sale-coupon/internal/config"
	"github.com/azizikri/flash-sale-coupon/internal/delivery/kafka"
	"github.com/azizikri/flash-sale-coupon/internal/repository"
	"github.com/azizikri/flash-sale-coupon/internal/usecase"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/twmb/franz-go/pkg/kgo"
)

var (
	testPool       *pgxpool.Pool
	testClient     *kgo.Client
	testCfg        *config.Config
	replyClient    *kgo.Client
	pendingReplies sync.Map
)

const testDSN = "postgresql://test:test@localhost:5433/coupondb_test?sslmode=disable"
const testKafkaBrokers = "localhost:5434"

func TestMain(m *testing.M) {
	if err := startServices(); err != nil {
		fmt.Printf("Failed to start services: %v\n", err)
		os.Exit(1)
	}

	if err := waitForPostgres(); err != nil {
		fmt.Printf("Postgres not ready: %v\n", err)
		stopServices()
		os.Exit(1)
	}

	if err := waitForKafka(); err != nil {
		fmt.Printf("Kafka not ready: %v\n", err)
		stopServices()
		os.Exit(1)
	}

	if err := runMigrations(); err != nil {
		fmt.Printf("Failed to run migrations: %v\n", err)
		stopServices()
		os.Exit(1)
	}

	var err error
	testPool, err = pgxpool.New(context.Background(), testDSN)
	if err != nil {
		fmt.Printf("Failed to create pool: %v\n", err)
		stopServices()
		os.Exit(1)
	}

	testCfg = &config.Config{
		KafkaBrokers:    testKafkaBrokers,
		KafkaInstanceID: "test-instance",
		KafkaGroupID:    "test-group",
		KafkaClientID:   "test-client",
	}

	testClient, err = kgo.NewClient(
		kgo.SeedBrokers(testKafkaBrokers),
		kgo.ClientID("test-integration"),
	)
	if err != nil {
		fmt.Printf("Failed to create kafka client: %v\n", err)
		stopServices()
		os.Exit(1)
	}

	if err := kafka.EnsureTopics(context.Background(), testClient, testCfg); err != nil {
		fmt.Printf("Failed to ensure topics: %v\n", err)
	}

	store := repository.New(testPool)
	service := usecase.NewCouponService(store)
	consumerClient, _ := kgo.NewClient(
		kgo.SeedBrokers(testKafkaBrokers),
		kgo.ConsumerGroup("test-consumers"),
		kgo.ConsumeTopics("coupon.create.req", "coupon.claim.req", "coupon.get.req"),
		kgo.DisableAutoCommit(),
	)
	consumer := kafka.NewConsumer(testCfg, consumerClient, service, store)
	go consumer.Start(context.Background())
	<-consumer.Ready()

	replyTopic := fmt.Sprintf("coupon.reply.%s", testCfg.KafkaInstanceID)
	replyClient, _ = kgo.NewClient(
		kgo.SeedBrokers(testKafkaBrokers),
		kgo.ConsumeTopics(replyTopic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtEnd()),
	)
	go func() {
		for {
			fetches := replyClient.PollFetches(context.Background())
			if fetches.IsClientClosed() {
				return
			}
			iter := fetches.RecordIter()
			for !iter.Done() {
				record := iter.Next()
				var resp kafka.ResponsePayload
				if err := json.Unmarshal(record.Value, &resp); err == nil {
					if ch, ok := pendingReplies.Load(resp.CorrelationID); ok {
						ch.(chan *kafka.ResponsePayload) <- &resp
					}
				}
			}
		}
	}()

	code := m.Run()

	testClient.Close()
	replyClient.Close()
	consumerClient.Close()
	testPool.Close()
	stopServices()

	os.Exit(code)
}

func startServices() error {
	cmd := exec.Command("docker-compose", "-f", "docker-compose.test.yml", "up", "-d")
	cmd.Dir = "../../../"
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func stopServices() {
	cmd := exec.Command("docker-compose", "-f", "docker-compose.test.yml", "down", "-v")
	cmd.Dir = "../../../"
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
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

func waitForKafka() error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for kafka")
		default:
			client, err := kgo.NewClient(kgo.SeedBrokers(testKafkaBrokers))
			if err == nil {
				err = client.Ping(ctx)
				client.Close()
				if err == nil {
					return nil
				}
			}
			time.Sleep(2 * time.Second)
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
	return repository.RunMigrations(pool, "../../../db/migrations")
}

func cleanupDB(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	_, _ = testPool.Exec(ctx, "DELETE FROM claims")
	_, _ = testPool.Exec(ctx, "DELETE FROM coupons")
}

func requestReply(t *testing.T, topic string, req kafka.RequestPayload) *kafka.ResponsePayload {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	ch := make(chan *kafka.ResponsePayload, 1)
	pendingReplies.Store(req.CorrelationID, ch)
	defer pendingReplies.Delete(req.CorrelationID)

	payload, _ := json.Marshal(req)
	record := &kgo.Record{
		Topic: topic,
		Value: payload,
	}
	if err := testClient.ProduceSync(ctx, record).FirstErr(); err != nil {
		t.Fatalf("failed to produce: %v", err)
	}

	select {
	case resp := <-ch:
		return resp
	case <-ctx.Done():
		t.Fatalf("timeout waiting for reply %s on topic %s", req.CorrelationID, topic)
		return nil
	}
}

func TestCreateCoupon_Success(t *testing.T) {
	cleanupDB(t)
	req := kafka.RequestPayload{
		SchemaVersion: 1,
		CorrelationID: uuid.New().String(),
		ReplyTo:       "coupon.reply.test-instance",
		Name:          "test-coupon",
		Amount:        100,
	}
	resp := requestReply(t, "coupon.create.req", req)
	if resp.Status != kafka.StatusSuccess {
		t.Errorf("expected SUCCESS, got %s: %s", resp.Status, resp.ErrorMessage)
	}
}

func TestCreateCoupon_Duplicate(t *testing.T) {
	cleanupDB(t)
	req := kafka.RequestPayload{
		SchemaVersion: 1,
		CorrelationID: uuid.New().String(),
		ReplyTo:       "coupon.reply.test-instance",
		Name:          "dup-coupon",
		Amount:        100,
	}
	requestReply(t, "coupon.create.req", req)

	req.CorrelationID = uuid.New().String()
	resp := requestReply(t, "coupon.create.req", req)

	if resp.Status != kafka.StatusError || resp.ErrorCode != kafka.ErrCodeDuplicateCoupon {
		t.Errorf("expected DUPLICATE_COUPON error, got %s (%s)", resp.Status, resp.ErrorCode)
	}
}

func TestClaimCoupon_Success(t *testing.T) {
	cleanupDB(t)
	requestReply(t, "coupon.create.req", kafka.RequestPayload{
		SchemaVersion: 1, CorrelationID: uuid.New().String(), ReplyTo: "coupon.reply.test-instance",
		Name: "claim-test", Amount: 10,
	})

	req := kafka.RequestPayload{
		SchemaVersion: 1,
		CorrelationID: uuid.New().String(),
		ReplyTo:       "coupon.reply.test-instance",
		UserID:        "user1",
		CouponName:    "claim-test",
	}
	resp := requestReply(t, "coupon.claim.req", req)
	if resp.Status != kafka.StatusSuccess {
		t.Errorf("expected SUCCESS, got %s: %s", resp.Status, resp.ErrorMessage)
	}
}

func TestClaimCoupon_AlreadyClaimed(t *testing.T) {
	cleanupDB(t)
	requestReply(t, "coupon.create.req", kafka.RequestPayload{
		SchemaVersion: 1, CorrelationID: uuid.New().String(), ReplyTo: "coupon.reply.test-instance",
		Name: "ac-test", Amount: 10,
	})

	claimReq := kafka.RequestPayload{
		SchemaVersion: 1, CorrelationID: uuid.New().String(), ReplyTo: "coupon.reply.test-instance",
		UserID: "user1", CouponName: "ac-test",
	}
	requestReply(t, "coupon.claim.req", claimReq)

	claimReq.CorrelationID = uuid.New().String()
	resp := requestReply(t, "coupon.claim.req", claimReq)

	if resp.Status != kafka.StatusError || resp.ErrorCode != kafka.ErrCodeAlreadyClaimed {
		t.Errorf("expected ALREADY_CLAIMED, got %s (%s)", resp.Status, resp.ErrorCode)
	}
}

func TestClaimCoupon_SoldOut(t *testing.T) {
	cleanupDB(t)
	requestReply(t, "coupon.create.req", kafka.RequestPayload{
		SchemaVersion: 1, CorrelationID: uuid.New().String(), ReplyTo: "coupon.reply.test-instance",
		Name: "so-test", Amount: 1,
	})

	requestReply(t, "coupon.claim.req", kafka.RequestPayload{
		SchemaVersion: 1, CorrelationID: uuid.New().String(), ReplyTo: "coupon.reply.test-instance",
		UserID: "user1", CouponName: "so-test",
	})

	resp := requestReply(t, "coupon.claim.req", kafka.RequestPayload{
		SchemaVersion: 1, CorrelationID: uuid.New().String(), ReplyTo: "coupon.reply.test-instance",
		UserID: "user2", CouponName: "so-test",
	})

	if resp.Status != kafka.StatusError || resp.ErrorCode != kafka.ErrCodeSoldOut {
		t.Errorf("expected SOLD_OUT, got %s (%s)", resp.Status, resp.ErrorCode)
	}
}

func TestGetCouponDetails_Success(t *testing.T) {
	cleanupDB(t)
	requestReply(t, "coupon.create.req", kafka.RequestPayload{
		SchemaVersion: 1, CorrelationID: uuid.New().String(), ReplyTo: "coupon.reply.test-instance",
		Name: "get-test", Amount: 100,
	})

	requestReply(t, "coupon.claim.req", kafka.RequestPayload{
		SchemaVersion: 1, CorrelationID: uuid.New().String(), ReplyTo: "coupon.reply.test-instance",
		UserID: "user1", CouponName: "get-test",
	})

	resp := requestReply(t, "coupon.get.req", kafka.RequestPayload{
		SchemaVersion: 1, CorrelationID: uuid.New().String(), ReplyTo: "coupon.reply.test-instance",
		CouponName: "get-test",
	})

	if resp.Status != kafka.StatusSuccess {
		t.Fatalf("expected SUCCESS, got %s", resp.Status)
	}
	if resp.Coupon.Name != "get-test" || resp.Coupon.RemainingAmount != 99 {
		t.Errorf("invalid coupon details: %+v", resp.Coupon)
	}
}

func TestConcurrency_50ClaimsFor5Stock(t *testing.T) {
	cleanupDB(t)
	requestReply(t, "coupon.create.req", kafka.RequestPayload{
		SchemaVersion: 1, CorrelationID: uuid.New().String(), ReplyTo: "coupon.reply.test-instance",
		Name: "concurrent-5", Amount: 5,
	})

	var wg sync.WaitGroup
	var successCount int32
	var conflictCount int32

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(uid int) {
			defer wg.Done()
			req := kafka.RequestPayload{
				SchemaVersion: 1, CorrelationID: uuid.New().String(), ReplyTo: "coupon.reply.test-instance",
				UserID: fmt.Sprintf("user%d", uid), CouponName: "concurrent-5",
			}
			resp := requestReply(t, "coupon.claim.req", req)
			if resp.Status == kafka.StatusSuccess {
				atomic.AddInt32(&successCount, 1)
			} else if resp.ErrorCode == kafka.ErrCodeSoldOut || resp.ErrorCode == kafka.ErrCodeAlreadyClaimed {
				atomic.AddInt32(&conflictCount, 1)
			}
		}(i)
	}
	wg.Wait()

	if successCount != 5 {
		t.Errorf("expected 5 successes, got %d", successCount)
	}
	if conflictCount != 45 {
		t.Errorf("expected 45 conflicts, got %d", conflictCount)
	}
}
