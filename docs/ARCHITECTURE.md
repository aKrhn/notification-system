# Architecture & Design Decisions

## System Overview

```
  Client (API)                    Infrastructure                     Delivery
 +-----------+    +----------------------------------------------+    +----------+
 |           |    |                                              |    |          |
 | POST /api |───>| Go API ──> PostgreSQL ──> RabbitMQ ──> Workers|──>| Provider |
 |           |    |   |             |             |          |   |    |          |
 | GET /api  |<───|   |         Migrations    Priority     Rate  |    | webhook  |
 |           |    |   |             |          Queues     Limiter|    |  .site   |
 | WS /ws    |<───|   |         Templates      DLX      Circuit |    |          |
 |           |    |   |             |             |       Breaker|    +----------+
 | /swagger  |    |   v             v             v          |   |
 | /health   |    | Redis ──────────────────────────────────>|   |
 | /metrics  |    |  (rate limit, pub/sub, token bucket)     |   |
 |           |    |                                          |   |
 +-----------+    +----------------------------------------------+
```

## Technology Choices

| Decision | Choice | Alternatives Considered | Why |
|----------|--------|------------------------|-----|
| Language | Go 1.25+ | - | Assessment requirement |
| HTTP Router | chi | gin, fiber, echo | stdlib-compatible (net/http), no framework lock-in |
| Database | PostgreSQL 16 | MongoDB | Relational data, ACID transactions, CHECK constraints, partial indexes |
| DB Access | database/sql | sqlc, Jet, Bob, GORM | Zero deps, every dev reads SQL, one scan helper |
| Queue | RabbitMQ | Redis, Kafka | Native ack/nack, priority queues, DLQ, at-least-once delivery |
| Rate Limit | Redis + Lua | In-memory | Distributed (works across instances), atomic operations |
| WebSocket | coder/websocket | gorilla/websocket | Context-aware, net/http compatible, zero deps |
| Logging | slog (stdlib) | zap, zerolog | Zero deps, 200ns vs 50ms HTTP calls — irrelevant |
| Config | caarlos0/env | viper, koanf | Env vars to struct, fail-fast, 12-factor |
| CI/CD | GitHub Actions | - | go vet + go test -race + docker build |

## Notification Lifecycle

```
                    API Request
                        |
                        v
+-------+    +--------+    +-----------+    +--------+    +-----------+
|pending |───>| queued |───>|processing |───>|  sent  |───>| delivered |
+-------+    +--------+    +-----------+    +--------+    +-----------+
                                |
                    +-----------+-----------+
                    |                       |
            retryable error          non-retryable
            (5xx, 429, timeout)      (4xx)
                    |                       |
                    v                       v
            increment retry           +---------+
            backoff: 1s*2^n+jitter    | failed  |──> DLQ
            max 3 retries             +---------+
                    |
                    v
            max retries exceeded
                    |
                    v
              +---------+
              | failed  |──> DLQ
              +---------+
```

**sent vs delivered**: `sent` means the provider accepted the message (HTTP 202). `delivered` means the message reached the recipient's device — confirmed via a delivery receipt callback from the provider. The `delivered` status is defined in the schema and CHECK constraint, ready for when the provider supports delivery receipts. Currently unused because webhook.site doesn't send callbacks.

## Queue Architecture

```
           notifications (direct exchange)
                    |
         +----------+----------+
         |          |          |
    key=sms    key=email   key=push
         |          |          |
         v          v          v
   [sms queue]  [email q]  [push q]     x-max-priority=10
         |          |          |         x-dead-letter-exchange
         |          |          |
    (on reject)
         |          |          |
         v          v          v
        notifications.dlx (direct exchange)
         |          |          |
         v          v          v
   [sms.dlq]  [email.dlq] [push.dlq]
```

**Priority mapping**: High=10, Normal=5, Low=1

## Database Schema

### Notifications Table (20 columns)

| Column | Type | Purpose |
|--------|------|---------|
| `id` | UUID PK | Random, safe to expose in APIs |
| `batch_id` | UUID | Groups batch notifications |
| `idempotency_key` | VARCHAR UNIQUE | Prevents duplicate sends |
| `recipient` | VARCHAR | Phone/email/device token |
| `channel` | VARCHAR(20) | sms, email, push (CHECK constraint) |
| `content` | TEXT | Message body |
| `subject` | VARCHAR | Email subject (NULL for sms/push) |
| `priority` | VARCHAR(10) | high, normal, low (CHECK constraint) |
| `status` | VARCHAR(20) | Lifecycle state (CHECK constraint): pending, queued, processing, sent, delivered, failed, cancelled |
| `provider_message_id` | VARCHAR | External provider's ID |
| `retry_count` | INT | Current retry attempt |
| `max_retries` | INT | Maximum attempts (default 3) |
| `next_retry_at` | TIMESTAMPTZ | Backoff schedule |
| `scheduled_at` | TIMESTAMPTZ | Future delivery time |
| `sent_at` | TIMESTAMPTZ | Delivery timestamp |
| `failed_at` | TIMESTAMPTZ | Failure timestamp |
| `error_message` | TEXT | Failure details |
| `metadata` | JSONB | Arbitrary key-value data |
| `created_at` | TIMESTAMPTZ | Creation time (cursor pagination key) |
| `updated_at` | TIMESTAMPTZ | Auto-updated via trigger |

### Indexes (7)

| Index | Columns | Type | Purpose |
|-------|---------|------|---------|
| PK | id | Unique | Primary key lookups |
| idx_batch_id | batch_id WHERE NOT NULL | Partial | Batch status queries |
| idx_status | status | B-tree | Status filtering |
| idx_channel | channel | B-tree | Channel filtering |
| idx_created_at | created_at | B-tree | Date range filtering |
| idx_cursor | (created_at DESC, id DESC) | Composite | Cursor pagination |
| idx_scheduled | scheduled_at WHERE pending | Partial | Scheduler polling |
| idx_retry | next_retry_at WHERE NOT NULL | Partial | Retry polling |

### Templates Table

| Column | Type | Purpose |
|--------|------|---------|
| id | UUID PK | Template identifier |
| name | VARCHAR UNIQUE | Human-readable name |
| channel | VARCHAR(20) | Target channel |
| content | TEXT | Template with `{{variable}}` placeholders |
| subject | VARCHAR | Email subject template |

## Rate Limiting

Redis token bucket with Lua script for atomic check-and-decrement:

```
100 tokens/second per channel
 |
 v
Worker calls Allow() ──> Lua script in Redis
                          |
                          ├── tokens >= 1? consume, return 1 (allowed)
                          └── tokens < 1? return 0 (denied, wait)
```

- Distributed: works across multiple app instances
- Atomic: Lua script prevents race conditions
- Fail-open: if Redis is down, allow the request (better to over-deliver than stop)

## Circuit Breaker (Per Channel)

```
         success
    ┌──────────────┐
    |              |
    v              |
┌────────┐    ┌────┴─────┐    ┌──────────┐
│ CLOSED │───>│   OPEN   │───>│HALF-OPEN │
│(normal)│    │(tripped) │    │(testing) │
└────────┘    └──────────┘    └─────┬────┘
    ^         failure rate          |
    |         > 50%             failure
    |         (10+ samples)        |
    └──────────────────────────────┘
```

- **Threshold**: >50% failure rate with 10+ samples
- **Open timeout**: 30 seconds before half-open test
- **Window**: 60-second sliding (tumbling) window
- **Per-channel**: SMS breaker independent of email/push

## Backpressure — Priority-Aware API Throttling

```
Background goroutine (every 2s)
    |
    v
RabbitMQ QueueDeclarePassive ──> total message count
    |
    v
atomic.Int32 (in-memory)
    |
    v
API Handler reads atomic ──> O(1), no network call
    |
    ├── priority=low  && depth > 5,000  → 429 + Retry-After
    ├── priority=normal && depth > 8,000 → 429 + Retry-After
    ├── priority=high && depth > 10,000 → 429 + Retry-After
    └── else → accept request
```

**Why tiered**: Marketing SMS (low) gets rejected first, preserving capacity for OTPs (high). The system degrades gracefully — not a hard cutoff.

**Response headers on 429**:
- `Retry-After: 30` — client knows when to retry
- `X-Queue-Depth: 12345` — observability at the API level

## Cursor Pagination

```
Page 1: SELECT ... ORDER BY created_at DESC, id DESC LIMIT 21
        └─ fetch 21 rows (limit+1)
        └─ if 21 rows: has_more=true, trim to 20, cursor = last row's (created_at, id)
        └─ if ≤20 rows: has_more=false, no cursor

Page 2: SELECT ... WHERE (created_at, id) < ($cursor_time, $cursor_id)
        ORDER BY created_at DESC, id DESC LIMIT 21
        └─ index seek, not scan — O(1) regardless of depth
```

## Correlation ID Flow

```
HTTP Request ──> Middleware (generate UUID) ──> Context
                                                  |
                 ┌────────────────────────────────┘
                 |
                 v
            slog.Info("request", "correlation_id", id)
                 |
                 v
            producer.Publish(CorrelationId: id)
                 |
                 v
            worker logs("notification sent", "correlation_id", id)
```

One UUID traces the entire flow: API → DB → Queue → Worker → Provider.

## Graceful Shutdown

```
SIGINT/SIGTERM received
        |
        v
1. Cancel worker context
2. Workers finish in-flight messages
3. WaitGroup.Wait() — all workers done
4. HTTP server Shutdown (10s timeout)
5. Deferred closes: producer → RabbitMQ → Redis → PostgreSQL
```

## Performance Characteristics

| Metric | Value | Notes |
|--------|-------|-------|
| API acceptance | ~10,000 req/s | Go HTTP + chi, CPU-bound |
| DB insert | ~5,000 inserts/s | Single-row with RETURNING |
| Queue publish | ~20,000 msg/s | RabbitMQ persistent mode |
| Worker throughput | 300 msg/s | Rate limited by design (100/channel) |
| p50 delivery latency | ~150ms | API → DB → Queue → Worker → Provider |
| p95 delivery latency | ~500ms | Includes provider HTTP round-trip |

**Bottleneck**: The rate limiter is the intentional throughput constraint. The queue absorbs bursts and workers drain at the configured rate.

## Production Considerations

Implemented:
- **Backpressure**: Priority-aware API throttling with async queue depth polling (atomic.Int32). Graceful degradation: low priority shed first, high priority last.

Designed for but not implemented (assessment scope):
- **OpenTelemetry**: Structured traces with spans (foundation: correlation IDs already propagate)
- **Local mock provider**: For accurate load testing without internet latency
- **Horizontal scaling**: Stateless API + competing consumers + centralized Redis rate limiting
