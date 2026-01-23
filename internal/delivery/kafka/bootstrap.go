package kafka

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/azizikri/flash-sale-coupon/internal/config"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

func EnsureTopics(ctx context.Context, client *kgo.Client, cfg *config.Config) error {
	adm := kadm.NewClient(client)

	topics := []string{
		TopicCreateRequest,
		TopicClaimRequest,
		TopicGetRequest,
		TopicCreateRetry,
		TopicClaimRetry,
		TopicGetRetry,
		TopicCreateRequest + TopicDLQSuffix,
		TopicClaimRequest + TopicDLQSuffix,
		TopicGetRequest + TopicDLQSuffix,
		fmt.Sprintf("%s%s", TopicReplyPrefix, cfg.KafkaInstanceID),
	}

	partitions := cfg.TopicPartitions()
	retryPartitions := cfg.RetryPartitions()
	replicationFactor := cfg.ReplicationFactor()

	for _, topic := range topics {
		p := partitions
		if strings.HasSuffix(topic, TopicRetrySuffix) || strings.HasSuffix(topic, TopicDLQSuffix) {
			p = retryPartitions
		}

		resp, err := adm.CreateTopics(ctx, int32(p), replicationFactor, nil, topic)
		if err != nil {
			return fmt.Errorf("failed to create topic %s: %w", topic, err)
		}
		for _, detail := range resp {
			if detail.Err != nil && !strings.Contains(detail.Err.Error(), "already exists") {
				return fmt.Errorf("failed to create topic %s: %w", detail.Topic, detail.Err)
			}
		}
	}

	log.Println("All topics ensured")
	return nil
}
