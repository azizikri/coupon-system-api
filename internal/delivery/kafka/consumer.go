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
	"strconv"
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
		c.sendError(ctx, record, ErrCodeInvalidRequest, "invalid request payload", 0)
		return
	}

	err := c.service.CreateCoupon(ctx, req.Name, req.Amount)
	var finalResp *ResponsePayload
	if err != nil {
		errorCode, message := mapCreateError(err)
		if errorCode == ErrCodeInternalError {
			c.sendToRetry(ctx, record, message)
		}
		finalResp = errorResponse(req.CorrelationID, errorCode, message)
	} else {
		finalResp = successResponse(req.CorrelationID, nil)
	}

	c.sendResponse(ctx, req.ReplyTo, finalResp)
}

func (c *Consumer) handleClaim(ctx context.Context, record *kgo.Record) {
	var req RequestPayload
	if err := json.Unmarshal(record.Value, &req); err != nil {
		c.sendError(ctx, record, ErrCodeInvalidRequest, "invalid request payload", 0)
		return
	}

	err := c.service.ClaimCoupon(ctx, req.UserID, req.CouponName)
	var finalResp *ResponsePayload
	if err != nil {
		errorCode, message := mapClaimError(err)
		if errorCode == ErrCodeInternalError {
			c.sendToRetry(ctx, record, message)
		}
		finalResp = errorResponse(req.CorrelationID, errorCode, message)
	} else {
		finalResp = successResponse(req.CorrelationID, nil)
	}

	c.sendResponse(ctx, req.ReplyTo, finalResp)
}

func (c *Consumer) handleGet(ctx context.Context, record *kgo.Record) {
	var req RequestPayload
	if err := json.Unmarshal(record.Value, &req); err != nil {
		c.sendError(ctx, record, ErrCodeInvalidRequest, "invalid request payload", 0)
		return
	}

	coupon, err := c.service.GetCouponDetails(ctx, req.CouponName)
	var finalResp *ResponsePayload
	if err != nil {
		errorCode, message := mapGetError(err)
		if errorCode == ErrCodeInternalError {
			c.sendToRetry(ctx, record, message)
		}
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

func (c *Consumer) sendError(ctx context.Context, record *kgo.Record, code, message string, attempts int) {
	var req RequestPayload
	_ = json.Unmarshal(record.Value, &req)

	resp := errorResponse(req.CorrelationID, code, message)
	if req.ReplyTo != "" {
		c.sendResponse(ctx, req.ReplyTo, resp)
	}

	c.sendToDLQ(ctx, record, message, attempts)
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

func retryAttempts(record *kgo.Record) int {
	for _, header := range record.Headers {
		if header.Key != RetryHeaderAttempts {
			continue
		}
		attempts, err := strconv.Atoi(string(header.Value))
		if err != nil {
			return 0
		}
		return attempts
	}
	return 0
}

func retryDelay(attempt int) time.Duration {
	if attempt <= 1 {
		return RetryBaseDelay
	}
	backoff := RetryBaseDelay * time.Duration(1<<uint(attempt-1))
	if backoff > RetryMaxDelay {
		return RetryMaxDelay
	}
	return backoff
}

func requestToRetryTopic(topic string) string {
	if strings.HasSuffix(topic, TopicRetrySuffix) {
		return topic
	}
	if strings.HasSuffix(topic, TopicRequestSuffix) {
		return strings.TrimSuffix(topic, TopicRequestSuffix) + TopicRetrySuffix
	}
	return topic + TopicRetrySuffix
}

func upsertHeader(headers []kgo.RecordHeader, key string, value []byte) []kgo.RecordHeader {
	for i := range headers {
		if headers[i].Key == key {
			headers[i].Value = value
			return headers
		}
	}
	return append(headers, kgo.RecordHeader{Key: key, Value: value})
}

func (c *Consumer) sendToRetry(ctx context.Context, record *kgo.Record, message string) bool {
	attempts := retryAttempts(record) + 1
	if attempts > MaxRetryAttempts {
		c.sendToDLQ(ctx, record, message, attempts)
		return false
	}

	nextAt := time.Now().Add(retryDelay(attempts)).Format(time.RFC3339)
	headers := upsertHeader(record.Headers, RetryHeaderNextAt, []byte(nextAt))
	headers = upsertHeader(headers, RetryHeaderAttempts, []byte(strconv.Itoa(attempts)))
	headers = upsertHeader(headers, ErrorHeaderKey, []byte(message))

	retryRecord := &kgo.Record{
		Topic:   requestToRetryTopic(record.Topic),
		Key:     record.Key,
		Value:   record.Value,
		Headers: headers,
	}
	if err := c.client.ProduceSync(ctx, retryRecord).FirstErr(); err != nil {
		log.Printf("Failed to enqueue retry record: %v", err)
		return false
	}
	return true
}

func (c *Consumer) sendToDLQ(ctx context.Context, record *kgo.Record, message string, attempts int) {
	headers := upsertHeader(record.Headers, ErrorHeaderKey, []byte(message))
	if attempts > 0 {
		headers = upsertHeader(headers, RetryHeaderAttempts, []byte(strconv.Itoa(attempts)))
	}

	dlqTopic := record.Topic + TopicDLQSuffix
	dlqRecord := &kgo.Record{
		Topic:   dlqTopic,
		Value:   record.Value,
		Headers: headers,
	}
	_ = c.client.ProduceSync(ctx, dlqRecord).FirstErr()
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
