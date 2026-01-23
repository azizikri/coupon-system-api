package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/azizikri/flash-sale-coupon/internal/config"
	"github.com/azizikri/flash-sale-coupon/internal/domain"
	"github.com/azizikri/flash-sale-coupon/internal/usecase"
	"github.com/google/uuid"
	"github.com/twmb/franz-go/pkg/kgo"
)

type Gateway struct {
	client      *kgo.Client
	cfg         *config.Config
	pendingResp sync.Map
}

func NewGateway(cfg *config.Config, client *kgo.Client) *Gateway {
	return &Gateway{
		client: client,
		cfg:    cfg,
	}
}

func (g *Gateway) CreateCoupon(ctx context.Context, name string, amount int) error {
	correlationID := uuid.New().String()
	req := RequestPayload{
		SchemaVersion: 1,
		CorrelationID: correlationID,
		ReplyTo:       fmt.Sprintf("%s%s", TopicReplyPrefix, g.cfg.KafkaInstanceID),
		Name:          name,
		Amount:        amount,
	}

	resp, err := g.requestReply(ctx, TopicCreateRequest, []byte(name), req)
	if err != nil {
		return err
	}
	if resp.Status == StatusError {
		return g.mapError(resp.ErrorCode, resp.ErrorMessage)
	}
	return nil
}

func (g *Gateway) ClaimCoupon(ctx context.Context, userID, couponName string) error {
	correlationID := uuid.New().String()
	req := RequestPayload{
		SchemaVersion: 1,
		CorrelationID: correlationID,
		ReplyTo:       fmt.Sprintf("%s%s", TopicReplyPrefix, g.cfg.KafkaInstanceID),
		UserID:        userID,
		CouponName:    couponName,
	}

	key := fmt.Sprintf("%s:%s", couponName, userID)
	resp, err := g.requestReply(ctx, TopicClaimRequest, []byte(key), req)
	if err != nil {
		return err
	}
	if resp.Status == StatusError {
		return g.mapError(resp.ErrorCode, resp.ErrorMessage)
	}
	return nil
}

func (g *Gateway) GetCouponDetails(ctx context.Context, name string) (*domain.Coupon, error) {
	correlationID := uuid.New().String()
	req := RequestPayload{
		SchemaVersion: 1,
		CorrelationID: correlationID,
		ReplyTo:       fmt.Sprintf("%s%s", TopicReplyPrefix, g.cfg.KafkaInstanceID),
		CouponName:    name,
	}

	resp, err := g.requestReply(ctx, TopicGetRequest, []byte(name), req)
	if err != nil {
		return nil, err
	}
	if resp.Status == StatusError {
		return nil, g.mapError(resp.ErrorCode, resp.ErrorMessage)
	}
	return resp.Coupon, nil
}

func (g *Gateway) requestReply(ctx context.Context, topic string, key []byte, req RequestPayload) (*ResponsePayload, error) {
	respChan := make(chan *ResponsePayload, 1)
	g.pendingResp.Store(req.CorrelationID, respChan)
	defer g.pendingResp.Delete(req.CorrelationID)

	payload, _ := json.Marshal(req)
	record := &kgo.Record{
		Topic: topic,
		Key:   key,
		Value: payload,
	}

	if err := g.client.ProduceSync(ctx, record).FirstErr(); err != nil {
		return nil, err
	}

	select {
	case resp := <-respChan:
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(RequestTimeout):
		return nil, errors.New("timeout waiting for response")
	}
}

func (g *Gateway) HandleResponse(payload []byte) {
	var resp ResponsePayload
	if err := json.Unmarshal(payload, &resp); err != nil {
		log.Printf("Failed to decode response payload: %v", err)
		return
	}

	if ch, ok := g.pendingResp.Load(resp.CorrelationID); ok {
		ch.(chan *ResponsePayload) <- &resp
		return
	}

	log.Printf("No pending response for correlation ID %s", resp.CorrelationID)
}

func (g *Gateway) mapError(code, message string) error {
	switch code {
	case ErrCodeDuplicateCoupon:
		return domain.ErrDuplicateCoupon
	case ErrCodeNotFound:
		return domain.ErrNotFound
	case ErrCodeAlreadyClaimed:
		return domain.ErrAlreadyClaimed
	case ErrCodeSoldOut:
		return domain.ErrSoldOut
	default:
		return errors.New(message)
	}
}

var _ usecase.CouponGateway = (*Gateway)(nil)
