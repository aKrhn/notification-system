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
| Language | Go 1.25+ | Required by assessment |
| HTTP Router | chi | stdlib-compatible, no framework lock-in |
| Database | PostgreSQL 16 | ACID transactions, CHECK constraints, partial indexes |
| Database Access | database/sql (stdlib) | Zero dependencies, every developer understands it |
| Message Broker | RabbitMQ | Reliable delivery with ack/nack, priority queues, DLQ |
| Cache / Rate Limit | Redis | Distributed token bucket, works across instances |
| WebSocket | coder/websocket | Context-aware, net/http compatible |
| Logging | slog (stdlib) | Structured JSON logging, zero dependency |
| API Docs | swag + Swagger UI | Auto-generated from Go annotations |
| Config | caarlos0/env | Env vars to typed struct, fail-fast |
| CI/CD | GitHub Actions | go vet + go test -race + docker build |

## Quick Start

```bash
# One command to start everything
docker-compose up -d

# Verify all services are running
docker-compose ps

# Check health
curl http://localhost:8080/health

# Open Swagger UI
open http://localhost:8080/swagger/index.html

# Open RabbitMQ Management
open http://localhost:15672  # guest/guest
```

The system starts: PostgreSQL, RabbitMQ, Redis, runs database migrations, and launches the application with 9 worker goroutines (3 per channel) + scheduler.

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

### Create with Template

```bash
# First create a template
curl -X POST http://localhost:8080/api/v1/templates \
  -H "Content-Type: application/json" \
  -d '{"name": "welcome_sms", "channel": "sms", "content": "Hello {{name}}, your code is {{code}}"}'

# Then use it
curl -X POST http://localhost:8080/api/v1/notifications \
  -H "Content-Type: application/json" \
  -d '{
    "recipient": "+905551234567",
    "channel": "sms",
    "template_id": "<template-id>",
    "variables": {"name": "John", "code": "1234"}
  }'
# Content will be: "Hello John, your code is 1234"
```

### Schedule for Future Delivery

```bash
curl -X POST http://localhost:8080/api/v1/notifications \
  -H "Content-Type: application/json" \
  -d '{
    "recipient": "+905551234567",
    "channel": "sms",
    "content": "Scheduled reminder",
    "scheduled_at": "2026-03-19T10:00:00Z"
  }'
# Status stays "pending" until scheduled_at, then scheduler publishes to queue
```

### Other Endpoints

```bash
# Get by ID
curl http://localhost:8080/api/v1/notifications/{id}

# Get batch status
curl http://localhost:8080/api/v1/notifications/batch/{batch_id}

# Cancel (only pending/queued)
curl -X PATCH http://localhost:8080/api/v1/notifications/{id}/cancel

# List with filters + cursor pagination
curl "http://localhost:8080/api/v1/notifications?status=sent&channel=sms&priority=high&limit=20"

# Health check (PostgreSQL + RabbitMQ + Redis)
curl http://localhost:8080/health

# Real-time metrics (queue depth, latency, circuit breakers, rate limiters)
curl http://localhost:8080/metrics

# WebSocket for live status updates
websocat ws://localhost:8080/api/v1/ws

# Templates CRUD
curl http://localhost:8080/api/v1/templates
```

Full API documentation available at **http://localhost:8080/swagger/index.html**.

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

- **Rate Limiting**: Redis token bucket (Lua script), 100 msg/s per channel
- **Circuit Breaker**: Per-channel, 3-state (closed/open/half-open). Trips at >50% failure rate (10+ samples). Open for 30s, then half-open test.
- **Exponential Backoff**: `delay = 1s * 2^attempt + random(0, 500ms)`. Attempts: ~1s, ~2s, ~4s.
- **Dead Letter Queue**: After max retries, notifications route to per-channel DLQ via RabbitMQ DLX.

## Testing

```bash
# Unit tests (76 test cases, no Docker needed)
go test -short ./...

# With race detection
go test -race -short ./...

# End-to-end tests (requires docker-compose up)
make test-e2e

# Load test (requires docker-compose up + k6)
k6 run scripts/load_test.js

# Benchmarks
go test -bench=. -benchmem ./internal/domain/... ./internal/repository/...
```

Test coverage: domain validation (16 cases), circuit breaker state machine (9 cases), exponential backoff, cursor pagination, provider error classification (12 cases), API handler contracts (20+ cases), benchmarks (8 cases).

## Project Structure

```
notification-system/
├── cmd/server/main.go                    # Entry point, wiring
├── internal/
│   ├── config/config.go                  # Environment-based configuration
│   ├── domain/
│   │   ├── notification.go               # Notification model, validation, constants
│   │   ├── template.go                   # Template model with {{variable}} rendering
│   │   └── errors.go                     # ErrNotFound, ErrConflict, ErrValidation
│   ├── api/
│   │   ├── router.go                     # Chi router with middleware
│   │   └── handler/                      # HTTP handlers
│   │       ├── notification.go           # 6 notification endpoints
│   │       ├── template.go              # Template CRUD
│   │       ├── health.go                # Health check (DB + RabbitMQ + Redis)
│   │       ├── metrics.go               # Real-time metrics
│   │       ├── websocket.go             # WebSocket status updates
│   │       └── response.go             # JSON response helpers
│   │   └── middleware/                   # Correlation ID, request logging
│   ├── service/notification.go           # Business logic + template resolution
│   ├── repository/
│   │   ├── notification.go              # Repository interface + cursor types
│   │   ├── template.go                  # Template repository interface
│   │   └── postgres/                    # PostgreSQL implementations
│   ├── queue/
│   │   ├── rabbitmq.go                  # Connection, exchange/queue declaration
│   │   └── producer.go                  # Priority-aware message publishing
│   ├── worker/
│   │   ├── dispatcher.go               # Per-channel worker pool lifecycle
│   │   └── processor.go                # Delivery, retry, error classification
│   ├── scheduler/scheduler.go           # Polls DB for scheduled notifications
│   ├── pubsub/redis.go                  # Redis pub/sub for WebSocket broadcasting
│   ├── provider/webhook.go              # webhook.site HTTP client
│   ├── ratelimiter/ratelimiter.go       # Redis token bucket (Lua script)
│   └── circuitbreaker/circuitbreaker.go # 3-state per-channel circuit breaker
├── migrations/                           # Versioned SQL (notifications + templates)
├── scripts/
│   ├── load_test.js                     # k6 load test (3 scenarios)
│   └── e2e_test.sh                      # End-to-end test suite (19 checks)
├── docs/                                # Generated Swagger spec
├── .github/workflows/ci.yml            # GitHub Actions CI pipeline
├── docker-compose.yml                   # One-command setup
├── Dockerfile                           # Multi-stage build
├── Makefile                             # Build, test, lint, migrate targets
└── README.md
```

## Bonus Features Implemented

| Feature | Status | Description |
|---------|--------|-------------|
| Failure Handling | Done | Retry with exponential backoff, circuit breaker, DLQ, error classification |
| Scheduled Notifications | Done | `scheduled_at` field, scheduler goroutine polls every 5s |
| Template System | Done | `{{variable}}` substitution, template CRUD API |
| WebSocket Updates | Done | Real-time status push via Redis pub/sub + coder/websocket |
| Distributed Tracing | Partial | Correlation ID propagation (HTTP → queue → worker) |
| GitHub Actions CI/CD | Done | go vet + go test -race + go build + docker build |

## Design Decisions

- **RabbitMQ over Redis for queuing**: Redis can't guarantee message delivery on crash. RabbitMQ has native ack/nack, priority queues, and dead letter exchanges.
- **database/sql over ORM/query builder**: Zero dependencies, every Go developer understands it, one scan helper handles the verbosity.
- **Cursor pagination over offset**: O(1) performance regardless of depth, consistent results when data changes.
- **Per-channel queues**: Independent scaling, rate limiting, circuit breakers, and DLQs per channel.
- **Best-effort queue publish**: If RabbitMQ is down after DB insert, notification stays pending. API returns 201. Scheduler reconciles.
- **Circuit breaker per channel**: SMS provider down doesn't stop email or push delivery.
- **Redis token bucket with Lua**: Atomic check-and-decrement, works across multiple app instances.
- **Correlation ID propagation**: HTTP request UUID flows through context → queue message → worker logs for end-to-end tracing.
