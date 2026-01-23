package config

import (
	"os"
	"strconv"
)

type Config struct {
	AppPort    string
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string
	DBSSLMode  string

	KafkaBrokers           string
	KafkaClientID          string
	KafkaGroupID           string
	KafkaReplyGroupID      string
	KafkaRetryGroupID      string
	KafkaInstanceID        string
	KafkaTopicPartitions   string
	KafkaRetryPartitions   string
	KafkaReplicationFactor string
	KafkaMinISR            string
	EventDrivenEnabled     string
}

func Load() *Config {
	instanceID := os.Getenv("KAFKA_INSTANCE_ID")
	if instanceID == "" {
		hostname, err := os.Hostname()
		if err != nil {
			instanceID = "unknown"
		} else {
			instanceID = hostname
		}
	}

	return &Config{
		AppPort:    getEnv("APP_PORT", "8080"),
		DBHost:     getEnv("DB_HOST", "localhost"),
		DBPort:     getEnv("DB_PORT", "5432"),
		DBUser:     getEnv("DB_USER", "postgres"),
		DBPassword: getEnv("DB_PASSWORD", "postgres"),
		DBName:     getEnv("DB_NAME", "coupondb"),
		DBSSLMode:  getEnv("DB_SSLMODE", "disable"),

		KafkaBrokers:           getEnv("KAFKA_BROKERS", "kafka:9092"),
		KafkaClientID:          getEnv("KAFKA_CLIENT_ID", "coupon-service"),
		KafkaGroupID:           getEnv("KAFKA_GROUP_ID", "coupon-consumers"),
		KafkaReplyGroupID:      getEnv("KAFKA_REPLY_GROUP_ID", "coupon-gateway-resp"),
		KafkaRetryGroupID:      getEnv("KAFKA_RETRY_GROUP_ID", "coupon-retry"),
		KafkaInstanceID:        instanceID,
		KafkaTopicPartitions:   getEnv("KAFKA_TOPIC_PARTITIONS", "3"),
		KafkaRetryPartitions:   getEnv("KAFKA_RETRY_PARTITIONS", "1"),
		KafkaReplicationFactor: getEnv("KAFKA_REPLICATION_FACTOR", "1"),
		KafkaMinISR:            getEnv("KAFKA_MIN_ISR", "1"),
		EventDrivenEnabled:     getEnv("EVENT_DRIVEN_ENABLED", "true"),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func (c *Config) TopicPartitions() int {
	return parseInt(c.KafkaTopicPartitions, 3)
}

func (c *Config) RetryPartitions() int {
	return parseInt(c.KafkaRetryPartitions, 1)
}

func (c *Config) ReplicationFactor() int16 {
	value := parseInt(c.KafkaReplicationFactor, 1)
	return int16(value)
}

func parseInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
