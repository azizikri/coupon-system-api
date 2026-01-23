package kafka

import "github.com/azizikri/flash-sale-coupon/internal/domain"

const (
	StatusSuccess = "SUCCESS"
	StatusError   = "ERROR"
)

const (
	ErrCodeDuplicateCoupon = "DUPLICATE_COUPON"
	ErrCodeNotFound        = "NOT_FOUND"
	ErrCodeAlreadyClaimed  = "ALREADY_CLAIMED"
	ErrCodeSoldOut         = "SOLD_OUT"
	ErrCodeInvalidRequest  = "INVALID_REQUEST"
	ErrCodeInternalError   = "INTERNAL_ERROR"
)

type RequestPayload struct {
	SchemaVersion int    `json:"schema_version"`
	CorrelationID string `json:"correlation_id"`
	ReplyTo       string `json:"reply_to"`
	Name          string `json:"name,omitempty"`
	Amount        int    `json:"amount,omitempty"`
	UserID        string `json:"user_id,omitempty"`
	CouponName    string `json:"coupon_name,omitempty"`
}

type ResponsePayload struct {
	SchemaVersion int            `json:"schema_version"`
	CorrelationID string         `json:"correlation_id"`
	Status        string         `json:"status"`
	ErrorCode     string         `json:"error_code,omitempty"`
	ErrorMessage  string         `json:"error_message,omitempty"`
	Coupon        *domain.Coupon `json:"coupon,omitempty"`
}
