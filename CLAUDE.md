# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run Commands

```bash
# Start full system (PostgreSQL, RabbitMQ, Redis, migrations, app)
docker compose up -d

# Rebuild after code changes
docker compose up -d --build

# Stop everything
docker compose down

# Build binary locally
go build -o bin/server ./cmd/server

# Run locally (requires env vars: DATABASE_URL, RABBITMQ_URL, REDIS_URL, WEBHOOK_URL)
./bin/server
```

## Testing

```bash
# Unit tests (76 tests, no Docker needed)
go test ./...

# With race detection
go test -race -short ./...

# Single package
go test -v ./internal/domain/...

# E2E tests (requires docker compose up)
make test-e2e

# Benchmarks
go test -bench=. -benchmem ./internal/domain/... ./internal/repository/...
```

## Migrations

```bash
# Create new migration
make migrate-create name=add_something

# Run migrations (done automatically by docker compose)
DATABASE_URL="postgres://notifications:notifications@localhost:5433/notifications?sslmode=disable" make migrate-up
```

## Swagger

```bash
# Regenerate after changing handler annotations
swag init -g cmd/server/main.go -o docs
# Or: make swagger
```

## Architecture

Event-driven notification system: API accepts notifications → stores in PostgreSQL → publishes to RabbitMQ → workers deliver via webhook.site provider.

### Request Flow
```
HTTP Request → chi router → handler → service → repository (PostgreSQL)
                                         ↓
                                    queue.Producer → RabbitMQ
                                         ↓
                              worker.Dispatcher → worker.Processor
                                         ↓
                              ratelimiter → circuitbreaker → provider (webhook.site)
                                         ↓
                              repository.UpdateSent/UpdateFailed
```

### Key Wiring (cmd/server/main.go)
All components are constructed and wired in `main.go`:
- DB → `postgres.New(db)` → repo
- RabbitMQ → `queue.NewConnection` → `queue.NewProducer` → producer
- Redis → rate limiters + pub/sub
- repo + producer → `service.NewNotificationService`
- Per-channel (sms/email/push): rate limiter + circuit breaker + processor → dispatcher
- Scheduler goroutine polls for scheduled notifications
- Background goroutine polls queue depth for backpressure (atomic.Int32)
- Graceful shutdown: cancel workers → wait → shutdown HTTP server

### Domain Layer (internal/domain/)
- `Notification` struct with 20 fields, pointer types for nullable columns
- `Template` struct with `Render(variables)` for `{{key}}` substitution
- Three error types: `ErrNotFound` (404), `ErrConflict` (409), `ErrValidation` (400 with field details)
- Validation: channel-specific content limits (SMS≤160, email≤100k, push≤4096)

### Repository Layer (internal/repository/)
- `NotificationRepository` interface with 10 methods
- PostgreSQL implementation uses `database/sql` (stdlib), not an ORM
- `scanNotification` helper maps 20 columns via `sql.NullString`/`sql.NullTime` → pointer types
- Dynamic WHERE clause building with `$1, $2...` argument tracking for List
- Cursor pagination: `(created_at, id) < ($cursor_time, $cursor_id)` with LIMIT+1

### Queue Layer (internal/queue/)
- `DeclareInfrastructure()` sets up: `notifications` exchange (direct), 3 channel queues with priority (x-max-priority=10), DLX exchange, 3 DLQ queues
- Producer uses mutex for thread-safe AMQP channel access
- Routing key = channel name (sms/email/push)

### Worker Layer (internal/worker/)
- Dispatcher: starts N goroutines per channel, QoS prefetch = workerCount
- Processor: unmarshal → fetch from DB → honor retry backoff → circuit breaker check → rate limiter wait → provider call → update status
- Backoff: `1s * 2^attempt + rand(0, 500ms)`, capped at 30s

### Handler Layer (internal/api/handler/)
- `notification.go`: 6 CRUD endpoints + backpressure guard (priority-aware 429 via atomic QueueDepth)
- `handler.QueueDepth` (atomic.Int32) is updated by background goroutine in main.go, read in O(1) by handlers
- Response helpers in `response.go`: `respondJSON`, `respondError` maps domain errors → HTTP status

### Config (internal/config/)
Required env vars: `DATABASE_URL`, `RABBITMQ_URL`, `REDIS_URL`, `WEBHOOK_URL`
Defaults: PORT=8080, WORKER_COUNT=3, MAX_RETRIES=3, RATE_LIMIT=100, LOG_LEVEL=info

## Infrastructure Ports (Docker Compose)

| Service | Host Port | Internal Port |
|---------|-----------|---------------|
| PostgreSQL | 5433 | 5432 |
| RabbitMQ AMQP | 5672 | 5672 |
| RabbitMQ Management | 15672 | 15672 |
| Redis | 6380 | 6379 |
| App | 8080 | 8080 |

## Conventions

- Domain errors (`ErrNotFound`, `ErrConflict`, `ErrValidation`) are struct types with pointer receivers for `errors.As` support
- Nullable DB columns: scan into `sql.NullString`/`sql.NullTime`, convert to `*string`/`*time.Time` in domain — no sql.Null types leak into domain
- JSON tags use `omitempty` for nullable fields, `swaggertype:"object"` for `json.RawMessage`
- Handler swagger annotations use `//	@Summary` format (tab-indented)
- All new repository methods need to be added to both the interface (`repository/notification.go`) and the PostgreSQL implementation (`repository/postgres/notification.go`)
