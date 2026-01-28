package kafka

import "time"

const (
	TopicCreateRequest = "coupon.create.req"
	TopicClaimRequest  = "coupon.claim.req"
	TopicGetRequest    = "coupon.get.req"
	TopicCreateRetry   = "coupon.create.retry"
	TopicClaimRetry    = "coupon.claim.retry"
	TopicGetRetry      = "coupon.get.retry"
	TopicReplyPrefix   = "coupon.reply."
	TopicRequestSuffix = ".req"
	TopicRetrySuffix   = ".retry"
	TopicDLQSuffix     = ".dlq"

	RequestTimeout   = 3 * time.Second
	MaxRetryAttempts = 3
	RetryBaseDelay   = 1 * time.Second
	RetryMaxDelay    = 30 * time.Second

	RetryHeaderNextAt   = "x-next-at"
	RetryHeaderAttempts = "x-retry-attempt"
	ErrorHeaderKey      = "x-error"
)
