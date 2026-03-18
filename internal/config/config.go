package config

import (
	"fmt"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	Port        int    `env:"PORT"         envDefault:"8080"`
	DatabaseURL string `env:"DATABASE_URL,required"`
	RabbitMQURL string `env:"RABBITMQ_URL,required"`
	RedisURL    string `env:"REDIS_URL,required"`
	WebhookURL  string `env:"WEBHOOK_URL,required"`

	WorkerCount int    `env:"WORKER_COUNT" envDefault:"3"`
	MaxRetries  int    `env:"MAX_RETRIES"  envDefault:"3"`
	RateLimit   int    `env:"RATE_LIMIT"   envDefault:"100"`

	LogLevel string `env:"LOG_LEVEL"    envDefault:"info"`
}

func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return cfg, nil
}
