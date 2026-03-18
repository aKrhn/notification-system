# Event-Driven Notification System

A scalable notification system that processes and delivers messages through multiple channels (SMS, Email, Push) with high throughput, reliable delivery, and real-time status tracking.

## Architecture

```
                         +------------------+
                         |    REST API       |
                         |  (chi router)     |
                         +--------+---------+
                                  |
                +-----------------+-----------------+
                |                 |                  |
                v                 v                  v
         +------------+   +------------+    +--------------+
         | PostgreSQL  |   |  RabbitMQ  |    |    Redis     |
         |  (storage)  |   | (queuing)  |    | (rate limit) |
         +------------+   +------+-----+    +--------------+
                                 |
                +-----------+----+----+-----------+
                |           |         |           |
                v           v         v           |
         +----------+ +----------+ +----------+  |
         |SMS Worker| |Email     | |Push      |  |
         |          | |Worker    | |Worker    |  |
         +----+-----+ +----+-----+ +----+-----+ |
              |             |            |        |
              +------+------+------+-----+        |
                     |             |               |
                     v             v               v
              +-----------+  +-----------+  +-----------+
              | Circuit   |  |  Rate     |  | Retry +   |
              | Breaker   |  |  Limiter  |  | Backoff   |
              +-----------+  +-----------+  +-----------+
                     |
                     v
              +--------------+
              | webhook.site  |
              |  (provider)   |
              +--------------+
```

## Tech Stack

| Component | Choice | Rationale |
|-----------|--------|-----------|
| Language | Go 1.22+ | Required by assessment |
| HTTP Router | chi | stdlib-compatible, no framework lock-in |
| Database | PostgreSQL 16 | ACID transactions, CHECK constraints, partial indexes |
| Database Access | database/sql (stdlib) | Zero dependencies, every developer understands it |
| Message Broker | RabbitMQ | Reliable delivery with ack/nack, priority queues, DLQ |
| Cache / Rate Limit | Redis | Distributed token bucket, works across instances |
| Logging | slog (stdlib) | Structured JSON logging, zero dependency |
| Config | caarlos0/env | Env vars to typed struct, fail-fast |

## Quick Start

```bash
# One command to start everything
docker-compose up -d

# Verify all services are running
docker-compose ps

# Check health
curl http://localhost:8080/health
```

The system starts: PostgreSQL, RabbitMQ, Redis, runs database migrations, and launches the application with 9 worker goroutines (3 per channel).

## API Reference

### Create Notification

```bash
curl -X POST http://localhost:8080/api/v1/notifications \
  -H "Content-Type: application/json" \
  -d '{
    "recipient": "+905551234567",
    "channel": "sms",
    "content": "Your verification code is 1234",
    "priority": "high"
  }'
```

Response (201):
```json
{
  "id": "f03fba2d-29c6-49b8-87aa-4bc022893d26",
  "recipient": "+905551234567",
  "channel": "sms",
  "content": "Your verification code is 1234",
  "priority": "high",
  "status": "queued",
  "retry_count": 0,
  "max_retries": 3,
  "created_at": "2026-03-18T08:18:36.670954Z",
  "updated_at": "2026-03-18T08:18:36.670954Z"
}
```

### Create Batch

```bash
curl -X POST http://localhost:8080/api/v1/notifications/batch \
  -H "Content-Type: application/json" \
  -d '{
    "notifications": [
      {"recipient": "user@example.com", "channel": "email", "content": "Welcome!", "subject": "Welcome"},
      {"recipient": "+905551234567", "channel": "sms", "content": "Your code: 5678"},
      {"recipient": "device-token-abc", "channel": "push", "content": "New message"}
    ]
  }'
```

Response (201):
```json
{
  "batch_id": "874e49d6-f6af-40fd-babe-7f578e4fffd4",
  "notifications": [...],
  "count": 3
}
```

### Get Notification

```bash
curl http://localhost:8080/api/v1/notifications/{id}
```

### Get Batch Status

```bash
curl http://localhost:8080/api/v1/notifications/batch/{batch_id}
```

### Cancel Notification

```bash
curl -X PATCH http://localhost:8080/api/v1/notifications/{id}/cancel
```

Only cancels notifications with status `pending` or `queued`. Returns 409 if already processing/sent.

### List Notifications

```bash
# With filters
curl "http://localhost:8080/api/v1/notifications?status=sent&channel=sms&limit=20"

# With cursor pagination
curl "http://localhost:8080/api/v1/notifications?cursor=eyJjIjoiMjAyNi0wMy0xOFQxMDowMDowMFoiLCJpIjoiYWJjLTEyMyJ9"

# All filter parameters
curl "http://localhost:8080/api/v1/notifications?status=pending&channel=email&priority=high&created_after=2026-03-01T00:00:00Z&created_before=2026-03-31T23:59:59Z&limit=50"
```

Response (200):
```json
{
  "data": [...],
  "pagination": {
    "next_cursor": "eyJjIjoiMjAyNi...",
    "has_more": true
  }
}
```

### Health Check

```bash
curl http://localhost:8080/health
```

Response (200):
```json
{
  "status": "healthy",
  "checks": {
    "database": "ok",
    "rabbitmq": "ok",
    "redis": "ok"
  }
}
```

### Metrics

```bash
curl http://localhost:8080/metrics
```

Response (200):
```json
{
  "timestamp": "2026-03-18T09:13:13Z",
  "queues": {"sms": {"depth": 5, "dlq_depth": 0}, ...},
  "notifications": {"sms": {"pending": 0, "sent": 234, "failed": 3}, ...},
  "circuit_breakers": {"sms": "closed", "email": "closed", "push": "closed"},
  "rate_limiters": {"sms": {"tokens_remaining": 95.2}, ...}
}
```

## Configuration

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `PORT` | No | 8080 | HTTP server port |
| `DATABASE_URL` | Yes | - | PostgreSQL connection string |
| `RABBITMQ_URL` | Yes | - | RabbitMQ AMQP URL |
| `REDIS_URL` | Yes | - | Redis connection URL |
| `WEBHOOK_URL` | Yes | - | External provider URL (webhook.site) |
| `WORKER_COUNT` | No | 3 | Workers per channel (total = 3 x channels) |
| `MAX_RETRIES` | No | 3 | Max delivery retry attempts |
| `RATE_LIMIT` | No | 100 | Max messages/second per channel |
| `LOG_LEVEL` | No | info | Log level (debug/info/warn/error) |

## Database Schema

```sql
CREATE TABLE notifications (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    batch_id            UUID,
    idempotency_key     VARCHAR(255) UNIQUE,
    recipient           VARCHAR(255) NOT NULL,
    channel             VARCHAR(20)  NOT NULL,     -- sms | email | push
    content             TEXT         NOT NULL,
    subject             VARCHAR(255),              -- required for email
    priority            VARCHAR(10)  DEFAULT 'normal', -- high | normal | low
    status              VARCHAR(20)  DEFAULT 'pending',
    provider_message_id VARCHAR(255),
    retry_count         INT          DEFAULT 0,
    max_retries         INT          DEFAULT 3,
    next_retry_at       TIMESTAMPTZ,
    scheduled_at        TIMESTAMPTZ,
    sent_at             TIMESTAMPTZ,
    failed_at           TIMESTAMPTZ,
    error_message       TEXT,
    metadata            JSONB,
    created_at          TIMESTAMPTZ  DEFAULT NOW(),
    updated_at          TIMESTAMPTZ  DEFAULT NOW()
);
```

Indexes: batch_id (partial), status, channel, (created_at DESC, id DESC) for cursor pagination, scheduled_at (partial), next_retry_at (partial).

## Delivery & Retry Logic

```
pending --> queued --> processing ---> sent
                          |
                          +-- retryable (429/5xx/timeout)
                          |   backoff: 1s * 2^attempt + jitter
                          |   max 3 retries
                          |
                          +-- non-retryable (4xx) --> failed
                          |
                          +-- max retries exceeded --> failed --> DLQ
```

- **Rate Limiting**: Redis token bucket, 100 msg/s per channel
- **Circuit Breaker**: Per-channel, 3-state (closed/open/half-open). Trips at >50% failure rate (10+ samples). Open for 30s, then half-open test.
- **Exponential Backoff**: `delay = 1s * 2^attempt + random(0, 500ms)`. Attempts: ~1s, ~2s, ~4s.

## Testing

```bash
# Unit tests (fast, no Docker needed)
go test -short ./...

# With race detection
go test -race -short ./...

# Load test (requires running system + k6 installed)
k6 run scripts/load_test.js
```

Tests cover: domain validation (16 cases), circuit breaker state machine (9 cases), exponential backoff calculation, cursor pagination encode/decode.

## Project Structure

```
notification-system/
├── cmd/server/main.go              # Entry point, wiring
├── internal/
│   ├── config/config.go            # Environment-based configuration
│   ├── domain/
│   │   ├── notification.go         # Domain models, validation, constants
│   │   └── errors.go               # ErrNotFound, ErrConflict, ErrValidation
│   ├── api/
│   │   ├── router.go               # Chi router with middleware
│   │   ├── handler/                # HTTP handlers (notification, health, metrics)
│   │   └── middleware/             # Correlation ID, request logging
│   ├── service/notification.go     # Business logic layer
│   ├── repository/
│   │   ├── notification.go         # Repository interface + cursor types
│   │   └── postgres/notification.go # PostgreSQL implementation
│   ├── queue/
│   │   ├── rabbitmq.go             # Connection, infrastructure declaration
│   │   └── producer.go             # Message publishing with priority
│   ├── worker/
│   │   ├── dispatcher.go           # Per-channel worker pool lifecycle
│   │   └── processor.go            # Message processing, retry, error handling
│   ├── provider/webhook.go         # HTTP client for webhook.site
│   ├── ratelimiter/ratelimiter.go  # Redis token bucket
│   └── circuitbreaker/circuitbreaker.go # 3-state circuit breaker
├── migrations/                     # Versioned SQL migrations
├── scripts/load_test.js            # k6 load test script
├── docker-compose.yml              # One-command setup
├── Dockerfile                      # Multi-stage build
├── Makefile                        # Build, test, lint targets
└── README.md
```

## Design Decisions

Key architectural decisions and their rationale:

- **RabbitMQ over Redis for queuing**: Redis can't guarantee message delivery on crash. RabbitMQ has native ack/nack, priority queues, and dead letter exchanges.
- **database/sql over ORM/query builder**: Zero dependencies, every Go developer understands it, one scan helper handles the verbosity.
- **Cursor pagination over offset**: O(1) performance regardless of depth, consistent results when data changes.
- **Per-channel queues**: Independent scaling, rate limiting, circuit breakers, and DLQs per channel.
- **Best-effort queue publish**: If RabbitMQ is down after DB insert, notification stays pending. API returns 201 (creation succeeded). Reconciliation handles retry.
- **Circuit breaker per channel**: SMS provider down doesn't stop email or push delivery.
