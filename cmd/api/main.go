package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/azizikri/flash-sale-coupon/internal/config"
	httphandler "github.com/azizikri/flash-sale-coupon/internal/delivery/http"
	"github.com/azizikri/flash-sale-coupon/internal/delivery/kafka"
	"github.com/azizikri/flash-sale-coupon/internal/repository"
	"github.com/azizikri/flash-sale-coupon/internal/usecase"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/twmb/franz-go/pkg/kgo"
)

func main() {
	cfg := config.Load()

	pool, err := initDB(cfg)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer pool.Close()

	if err := runMigrations(cfg); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	store := repository.New(pool)
	service := usecase.NewCouponService(store)

	var gateway usecase.CouponGateway
	var kafkaClient *kgo.Client
	var consumer *kafka.Consumer
	var replyConsumer *kgo.Client
	var retryClient *kgo.Client

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cfg.EventDrivenEnabled == "true" {
		brokers := strings.Split(cfg.KafkaBrokers, ",")
		kafkaClient, err = newConsumerClient(
			brokers,
			cfg.KafkaClientID,
			cfg.KafkaGroupID,
			kafka.TopicCreateRequest,
			kafka.TopicClaimRequest,
			kafka.TopicGetRequest,
		)
		if err != nil {
			log.Fatalf("Failed to create kafka client: %v", err)
		}

		if err := kafka.EnsureTopics(ctx, kafkaClient, cfg); err != nil {
			log.Printf("Warning: failed to ensure topics: %v", err)
		}

		kgateway := kafka.NewGateway(cfg, kafkaClient)
		gateway = kgateway

		consumer = kafka.NewConsumer(cfg, kafkaClient, service, store)
		go consumer.Start(ctx)

		retryClient, err = newConsumerClient(
			brokers,
			cfg.KafkaClientID+"-retry",
			cfg.KafkaRetryGroupID,
			kafka.TopicCreateRetry,
			kafka.TopicClaimRetry,
			kafka.TopicGetRetry,
		)
		if err != nil {
			log.Fatalf("Failed to create retry kafka client: %v", err)
		}
		retryConsumer := kafka.NewConsumer(cfg, retryClient, service, store)
		go retryConsumer.StartRetry(ctx)

		replyTopic := fmt.Sprintf("%s%s", kafka.TopicReplyPrefix, cfg.KafkaInstanceID)
		replyConsumer, err = newReplyClient(
			brokers,
			cfg.KafkaClientID+"-reply",
			replyTopic,
		)
		if err != nil {
			log.Fatalf("Failed to create reply kafka client: %v", err)
		}

		startReplyPoller(ctx, replyConsumer, kgateway)
	} else {
		gateway = kafka.NewDirectGateway(service)
	}

	handler := httphandler.NewHandler(gateway)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	handler.Routes(r)

	srv := &http.Server{
		Addr:    ":" + cfg.AppPort,
		Handler: r,
	}

	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Printf("Starting server on port %s", cfg.AppPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Server failed: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("Shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP shutdown error: %v", err)
	}

	if kafkaClient != nil {
		kafkaClient.Close()
	}
	if replyConsumer != nil {
		replyConsumer.Close()
	}
	if retryClient != nil {
		retryClient.Close()
	}

	wg.Wait()
	log.Println("Shutdown complete")
}

func initDB(cfg *config.Config) (*pgxpool.Pool, error) {
	connStr := fmt.Sprintf(
		"postgresql://%s:%s@%s:%s/%s?sslmode=%s",
		cfg.DBUser,
		cfg.DBPassword,
		cfg.DBHost,
		cfg.DBPort,
		cfg.DBName,
		cfg.DBSSLMode,
	)

	pool, err := pgxpool.New(context.Background(), connStr)
	if err != nil {
		return nil, fmt.Errorf("unable to create connection pool: %w", err)
	}

	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		return nil, fmt.Errorf("unable to ping database: %w", err)
	}

	return pool, nil
}

func runMigrations(cfg *config.Config) error {
	pool, err := initDB(cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	return repository.RunMigrations(pool, "db/migrations")
}

func newConsumerClient(brokers []string, clientID, groupID string, topics ...string) (*kgo.Client, error) {
	return kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID(clientID),
		kgo.ConsumerGroup(groupID),
		kgo.ConsumeTopics(topics...),
		kgo.DisableAutoCommit(),
	)
}

func newReplyClient(brokers []string, clientID, topic string) (*kgo.Client, error) {
	return kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID(clientID),
		kgo.ConsumeTopics(topic),
	)
}

func startReplyPoller(ctx context.Context, client *kgo.Client, gateway *kafka.Gateway) {
	go func() {
		for {
			fetches := client.PollFetches(ctx)
			if fetches.IsClientClosed() {
				return
			}
			iter := fetches.RecordIter()
			for !iter.Done() {
				record := iter.Next()
				gateway.HandleResponse(record.Value)
			}
		}
	}()
}
