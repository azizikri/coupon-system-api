package kafka

import (
	"context"
	"encoding/json"
	"github.com/azizikri/flash-sale-coupon/internal/config"
	"github.com/azizikri/flash-sale-coupon/internal/domain"
	"github.com/azizikri/flash-sale-coupon/internal/repository"
	"github.com/azizikri/flash-sale-coupon/internal/usecase"
	"github.com/twmb/franz-go/pkg/kgo"
	"log"
	"strings"
	"time"
)

type Consumer struct {
	client  *kgo.Client
	cfg     *config.Config
	service *usecase.CouponService
	store   repository.Store
	gateway *Gateway
	ready   chan struct{}
}

func NewConsumer(cfg *config.Config, client *kgo.Client, service *usecase.CouponService, store repository.Store) *Consumer {
	return &Consumer{
		client:  client,
		cfg:     cfg,
		service: service,
		store:   store,
		ready:   make(chan struct{}),
	}
}

func (c *Consumer) Start(ctx context.Context) {
	close(c.ready)
	for {
		fetches := c.client.PollFetches(ctx)
		if fetches.IsClientClosed() {
			return
		}
		if errs := fetches.Errors(); len(errs) > 0 {
			log.Printf("Consumer poll errors: %v", errs)
		}

		iter := fetches.RecordIter()
		for !iter.Done() {
			record := iter.Next()
			c.processRecord(ctx, record)
		}

		if err := c.client.CommitRecords(ctx, fetches.Records()...); err != nil {
			log.Printf("Failed to commit records: %v", err)
		}
	}
}

func (c *Consumer) StartRetry(ctx context.Context) {
	for {
		fetches := c.client.PollFetches(ctx)
		if fetches.IsClientClosed() {
			return
		}
		iter := fetches.RecordIter()
		for !iter.Done() {
			record := iter.Next()

			if nextAt, ok := retryNextAt(record); ok && time.Now().Before(nextAt) {
				time.Sleep(time.Until(nextAt))
			}

			mainTopic := strings.TrimSuffix(record.Topic, TopicRetrySuffix) + TopicRequestSuffix
			newRecord := &kgo.Record{
				Topic:   mainTopic,
				Key:     record.Key,
				Value:   record.Value,
				Headers: record.Headers,
			}
			if err := c.client.ProduceSync(ctx, newRecord).FirstErr(); err != nil {
				log.Printf("Failed to requeue retry record: %v", err)
			}
		}
		if err := c.client.CommitRecords(ctx, fetches.Records()...); err != nil {
			log.Printf("Failed to commit retry records: %v", err)
		}
	}
}

func (c *Consumer) Ready() <-chan struct{} {
	return c.ready
}

func (c *Consumer) processRecord(ctx context.Context, record *kgo.Record) {
	switch record.Topic {
	case TopicCreateRequest:
		c.handleCreate(ctx, record)
	case TopicClaimRequest:
		c.handleClaim(ctx, record)
	case TopicGetRequest:
		c.handleGet(ctx, record)
	}
}

func (c *Consumer) handleCreate(ctx context.Context, record *kgo.Record) {
	var req RequestPayload
	if err := json.Unmarshal(record.Value, &req); err != nil {
		c.sendError(ctx, record, ErrCodeInvalidRequest, "invalid request payload")
		return
	}

	err := c.service.CreateCoupon(ctx, req.Name, req.Amount)
	var finalResp *ResponsePayload
	if err != nil {
		errorCode, message := mapCreateError(err)
		finalResp = errorResponse(req.CorrelationID, errorCode, message)
	} else {
		finalResp = successResponse(req.CorrelationID, nil)
	}

	c.sendResponse(ctx, req.ReplyTo, finalResp)
}

func (c *Consumer) handleClaim(ctx context.Context, record *kgo.Record) {
	var req RequestPayload
	if err := json.Unmarshal(record.Value, &req); err != nil {
		c.sendError(ctx, record, ErrCodeInvalidRequest, "invalid request payload")
		return
	}

	err := c.service.ClaimCoupon(ctx, req.UserID, req.CouponName)
	var finalResp *ResponsePayload
	if err != nil {
		errorCode, message := mapClaimError(err)
		finalResp = errorResponse(req.CorrelationID, errorCode, message)
	} else {
		finalResp = successResponse(req.CorrelationID, nil)
	}

	c.sendResponse(ctx, req.ReplyTo, finalResp)
}

func (c *Consumer) handleGet(ctx context.Context, record *kgo.Record) {
	var req RequestPayload
	if err := json.Unmarshal(record.Value, &req); err != nil {
		c.sendError(ctx, record, ErrCodeInvalidRequest, "invalid request payload")
		return
	}

	coupon, err := c.service.GetCouponDetails(ctx, req.CouponName)
	var finalResp *ResponsePayload
	if err != nil {
		errorCode, message := mapGetError(err)
		finalResp = errorResponse(req.CorrelationID, errorCode, message)
	} else {
		finalResp = successResponse(req.CorrelationID, coupon)
	}

	c.sendResponse(ctx, req.ReplyTo, finalResp)
}

func (c *Consumer) sendResponse(ctx context.Context, topic string, resp *ResponsePayload) {
	payload, _ := json.Marshal(resp)
	record := &kgo.Record{
		Topic: topic,
		Value: payload,
	}
	if err := c.client.ProduceSync(ctx, record).FirstErr(); err != nil {
		log.Printf("Failed to send response to %s: %v", topic, err)
	}
}

func (c *Consumer) sendError(ctx context.Context, record *kgo.Record, code, message string) {
	var req RequestPayload
	_ = json.Unmarshal(record.Value, &req)

	resp := errorResponse(req.CorrelationID, code, message)
	if req.ReplyTo != "" {
		c.sendResponse(ctx, req.ReplyTo, resp)
	}

	dlqTopic := record.Topic + TopicDLQSuffix
	dlqRecord := &kgo.Record{
		Topic: dlqTopic,
		Value: record.Value,
		Headers: []kgo.RecordHeader{
			{Key: ErrorHeaderKey, Value: []byte(message)},
		},
	}
	_ = c.client.ProduceSync(ctx, dlqRecord).FirstErr()
}

func retryNextAt(record *kgo.Record) (time.Time, bool) {
	for _, header := range record.Headers {
		if header.Key != RetryHeaderNextAt {
			continue
		}
		nextAt, err := time.Parse(time.RFC3339, string(header.Value))
		if err != nil {
			return time.Time{}, false
		}
		return nextAt, true
	}

	return time.Time{}, false
}

func successResponse(correlationID string, coupon *domain.Coupon) *ResponsePayload {
	return &ResponsePayload{
		SchemaVersion: 1,
		CorrelationID: correlationID,
		Status:        StatusSuccess,
		Coupon:        coupon,
	}
}

func errorResponse(correlationID, code, message string) *ResponsePayload {
	return &ResponsePayload{
		SchemaVersion: 1,
		CorrelationID: correlationID,
		Status:        StatusError,
		ErrorCode:     code,
		ErrorMessage:  message,
	}
}

func mapCreateError(err error) (string, string) {
	code := ErrCodeInternalError
	message := err.Error()
	if message == "coupon already exists" {
		code = ErrCodeDuplicateCoupon
	}
	return code, message
}

func mapClaimError(err error) (string, string) {
	code := ErrCodeInternalError
	message := err.Error()
	switch message {
	case "coupon not found":
		code = ErrCodeNotFound
	case "user has already claimed this coupon":
		code = ErrCodeAlreadyClaimed
	case "coupon is sold out":
		code = ErrCodeSoldOut
	}
	return code, message
}

func mapGetError(err error) (string, string) {
	code := ErrCodeInternalError
	message := err.Error()
	if message == "coupon not found" {
		code = ErrCodeNotFound
	}
	return code, message
}
