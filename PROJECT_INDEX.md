# Project Index: Event-Driven Notification System

Generated: 2026-03-19

## Project Structure

```
notification-system/
├── cmd/server/main.go                         # Entry point — all wiring
├── internal/
│   ├── config/config.go                       # Env-based config (caarlos0/env)
│   ├── domain/
│   │   ├── notification.go                    # Notification struct, validation, constants
│   │   ├── template.go                        # Template struct, {{var}} rendering
│   │   └── errors.go                          # ErrNotFound, ErrConflict, ErrValidation
│   ├── api/
│   │   ├── router.go                          # Chi router, middleware, routes
│   │   ├── handler/
│   │   │   ├── notification.go                # 6 CRUD endpoints + backpressure
│   │   │   ├── template.go                    # Template CRUD
│   │   │   ├── health.go                      # DB + RabbitMQ + Redis checks
│   │   │   ├── metrics.go                     # Queue depth, latency, breakers
│   │   │   ├── websocket.go                   # Real-time status via Redis pub/sub
│   │   │   └── response.go                    # JSON helpers, error mapping
│   │   └── middleware/
│   │       ├── correlation.go                 # UUID per request, X-Correlation-ID
│   │       └── logging.go                     # slog request logging
│   ├── service/notification.go                # Business logic, template resolution
│   ├── repository/
│   │   ├── notification.go                    # Interface (10 methods) + cursor types
│   │   ├── template.go                        # Template interface
│   │   └── postgres/
│   │       ├── notification.go                # Full SQL implementation + scan helper
│   │       └── template.go                    # Template SQL implementation
│   ├── queue/
│   │   ├── rabbitmq.go                        # Connection, exchange/queue/DLQ setup
│   │   └── producer.go                        # Priority-aware publishing
│   ├── worker/
│   │   ├── dispatcher.go                      # Per-channel worker pool lifecycle
│   │   └── processor.go                       # Delivery, retry, circuit breaker, rate limit
│   ├── scheduler/scheduler.go                 # Polls DB for scheduled notifications
│   ├── pubsub/redis.go                        # Redis pub/sub for WebSocket
│   ├── provider/webhook.go                    # HTTP client, error classification
│   ├── ratelimiter/ratelimiter.go             # Redis token bucket (Lua script)
│   └── circuitbreaker/circuitbreaker.go       # 3-state per-channel breaker
├── migrations/
│   ├── 000001_create_notifications.{up,down}.sql
│   └── 000002_create_templates.{up,down}.sql
├── scripts/
│   ├── e2e_test.sh                            # 19 live E2E checks
│   └── load_test.js                           # k6 load test (3 scenarios)
├── docs/
│   ├── ARCHITECTURE.md                        # System design & trade-offs
│   ├── swagger.json                           # Generated OpenAPI spec
│   └── swagger.yaml
├── .github/workflows/ci.yml                   # go vet + test + build + docker
├── docker-compose.yml                         # PostgreSQL, RabbitMQ, Redis, app
├── Dockerfile                                 # Multi-stage (golang:1.26 → alpine:3.21)
├── Makefile                                   # build, test, test-e2e, swagger, docker
└── README.md
```

## Entry Points

- **CLI/Server**: `cmd/server/main.go` — loads config, connects DB/RabbitMQ/Redis, starts workers + scheduler + HTTP server
- **Tests**: `go test ./...` — 76 unit tests across 6 packages
- **E2E**: `./scripts/e2e_test.sh` — 19 live checks against running system
- **Load**: `k6 run scripts/load_test.js` — burst traffic simulation

## Core Modules

| Module | Path | Purpose |
|--------|------|---------|
| domain | `internal/domain/` | Notification + Template structs, validation, error types |
| handler | `internal/api/handler/` | HTTP endpoints (notification, template, health, metrics, WS) |
| repository | `internal/repository/postgres/` | PostgreSQL CRUD, dynamic filtering, cursor pagination |
| queue | `internal/queue/` | RabbitMQ connection, exchange/queue declaration, publishing |
| worker | `internal/worker/` | Dispatcher (goroutine pool) + Processor (delivery lifecycle) |
| ratelimiter | `internal/ratelimiter/` | Redis token bucket with Lua script |
| circuitbreaker | `internal/circuitbreaker/` | 3-state machine (closed/open/half-open) |
| scheduler | `internal/scheduler/` | Polls for scheduled notifications every 5s |
| pubsub | `internal/pubsub/` | Redis pub/sub for WebSocket broadcasting |
| provider | `internal/provider/` | webhook.site HTTP client with error classification |

## API Endpoints

| Method | Path | Handler | Purpose |
|--------|------|---------|---------|
| POST | /api/v1/notifications | Create | Create single notification |
| POST | /api/v1/notifications/batch | CreateBatch | Batch (up to 1000) |
| GET | /api/v1/notifications/{id} | GetByID | Query by ID |
| GET | /api/v1/notifications/batch/{id} | GetBatchStatus | Batch status |
| PATCH | /api/v1/notifications/{id}/cancel | Cancel | Cancel pending/queued |
| GET | /api/v1/notifications | List | Filter + cursor pagination |
| POST | /api/v1/templates | Create | Create template |
| GET | /api/v1/templates | List | List templates |
| GET | /api/v1/templates/{id} | GetByID | Get template |
| GET | /health | Health | DB + RabbitMQ + Redis |
| GET | /metrics | Metrics | Queue depth, latency, breakers |
| GET | /swagger/* | Swagger UI | API documentation |
| GET | /api/v1/ws | WebSocket | Real-time status updates |

## Dependencies (Direct)

| Package | Version | Purpose |
|---------|---------|---------|
| go-chi/chi/v5 | v5.2.5 | HTTP router |
| caarlos0/env/v11 | v11.4.0 | Config from env vars |
| google/uuid | v1.6.0 | UUID generation |
| lib/pq | v1.11.2 | PostgreSQL driver |
| rabbitmq/amqp091-go | v1.10.0 | RabbitMQ client |
| redis/go-redis/v9 | v9.18.0 | Redis client |
| coder/websocket | v1.8.14 | WebSocket |
| swaggo/swag | v1.16.6 | Swagger generation |
| swaggo/http-swagger | v1.3.4 | Swagger UI |
| stretchr/testify | v1.7.0 | Test assertions |

## Test Coverage

| Package | Tests | What |
|---------|-------|------|
| domain | 19 | Validation, errors, benchmarks |
| circuitbreaker | 9 | State transitions |
| worker | 5 | Backoff calculation |
| repository | 7 | Cursor encode/decode, benchmarks |
| api/handler | 24 | HTTP contracts, mock repo |
| provider | 12 | Error classification via httptest |
| **Total** | **76** | |

## Quick Start

```bash
docker-compose up -d          # Start everything
curl http://localhost:8080/health  # Verify
go test ./...                  # Run tests
make test-e2e                  # E2E tests (needs Docker)
open http://localhost:8080/swagger/index.html  # API docs
```
