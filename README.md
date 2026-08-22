# Embodied Reading Studio

Embodied Reading Studio is a production-oriented Go backend for humanities study groups that preserve direct, physical reading as a first-class research process. It coordinates physical-edition registration, time-bounded page reservations, offline close-reading sessions, observation notes, quotation claims, independent review, seminar disposition, and auditable archive snapshots.

The system does not summarize books, generate interpretations, lend books, publish a CMS, or track reading habits. Its job is to preserve who read which physical edition, under what program and resource ownership, what evidence was recorded, how independent review concluded, and when the resulting research record became safe to archive.

## Architecture

The repository is a single Go module with explicit ownership boundaries:

```text
cmd/server                    HTTP and worker process entry point
internal/domain              Aggregate state machines and value rules
internal/service             Cross-aggregate transaction orchestration
internal/repository          Persistence and transaction contracts
internal/storage/sqlite      SQLite implementation and migrations
internal/httpapi             Public JSON API and stable error contract
internal/middleware          Request ID, authentication, logging, recovery
internal/worker              Outbox delivery and maintenance lifecycle
internal/audit               Persistent audit event model
internal/config              Environment-backed configuration
internal/platform            Clock and ID providers
migrations                   Versioned relational schema
```

Domain packages do not depend on HTTP or SQLite. HTTP handlers do not execute SQL. Services use repository interfaces and explicit `WithinTx` callbacks for multi-entity operations. Workers call services for business state changes and use leases and fencing tokens for ownership.

## Business Flows

The primary study flow is:

1. A coordinator registers a work and a fingerprinted physical edition.
2. The coordinator creates a time-bounded study program and opens enrollment.
3. Active researchers join with route-scoped idempotency keys.
4. Members reserve non-overlapping page sections within the program window.
5. A member starts an offline reading session, taking a fenced lease on the page assignment.
6. Submission atomically persists the session state, observations, quotation claims, assignment release, audit record, and outbox events.
7. An independent reviewer claims a review lease and records a decision.
8. Rejected claims may enter a separately assigned dispute process.
9. Verified evidence becomes a seminar agenda; every item needs a disposition before closing.
10. A program can be archived only after reading, evidence, disputes, agenda items, and worker leases are terminal.

The recovery flow is:

1. Outbox and archive work is persisted before execution.
2. A worker claims one due item with a lease owner, monotonically increasing fencing token, and expiry.
3. Completion is accepted only from the current unexpired owner.
4. Failures persist the error and a bounded retry schedule.
5. Process restart recovers queued, retryable, or expired work from SQLite.

## Data Model

SQLite runs in WAL mode with foreign keys, a busy timeout, and real SQL transactions. The initial migration creates related tables for:

- tenants, users, and revocable authentication sessions;
- works and fingerprinted physical editions;
- study programs and program membership;
- page-section assignments and reading sessions;
- observation notes and quotation claims;
- review assignments, decisions, and claim disputes;
- seminar agendas and agenda items;
- archive jobs and immutable archive snapshots;
- outbox events, idempotency records, and audit events.

Important relationships are protected by primary keys, foreign keys, unique constraints, state fields, time fields, and business indexes. Optimistic updates include an expected version. Resource claims use lease owner, lease token, and lease expiry together.

Migration version `1` can create an empty database. Repeated startup first reads `schema_migrations` and does not replay the schema. A conflicting name for an existing migration version blocks startup instead of deleting or rewriting historical data.

## Authentication

The server stores opaque session tokens only as SHA-256 hashes and stores passwords with bcrypt. Sessions have absolute expiry, last-seen throttling, revocation, and optimistic versions. Logout revokes the current session; account deactivation and revocation are persistent across process restarts.

The default bootstrap coordinator is intended only for local startup. Set `BOOTSTRAP_EMAIL` and `BOOTSTRAP_PASSWORD` before using a shared environment.

## HTTP Contract

All business routes use `/v1`. Authentication uses `Authorization: Bearer <token>`. Requests accept an optional `X-Request-ID`; the server creates one when absent and returns it in both headers and error payloads.

Errors use:

```json
{
  "code": "page_section_already_reserved",
  "message": "requested page section overlaps an active reservation",
  "request_id": "request_..."
}
```

Stable categories map to validation, unauthorized, forbidden, not found, conflict, precondition, deadline, cancellation, dependency, and internal responses. Internal causes are logged but not exposed to clients.

Health endpoints:

- `GET /health/live` reports process liveness.
- `GET /health/ready` checks the required database dependency with a bounded context.

Representative business endpoints:

- `POST /v1/login` and `POST /v1/logout`
- `POST /v1/editions`
- `POST /v1/programs`
- `POST /v1/programs/{programID}/open`
- `POST /v1/programs/{programID}/join`
- `POST /v1/programs/{programID}/start`
- `POST /v1/programs/{programID}/assignments`
- `POST /v1/readings`
- `POST /v1/readings/{readingID}/submit`
- `POST /v1/claims/{claimID}/reviews`
- `POST /v1/reviews/{reviewID}/claim`
- `POST /v1/reviews/{reviewID}/decide`
- `POST /v1/claims/{claimID}/disputes`
- `POST /v1/agendas/{agendaID}/close`
- `POST /v1/programs/{programID}/archive`

## Configuration

Copy values from `.env.example` into the environment. The server reads:

| Variable | Default | Purpose |
| --- | --- | --- |
| `LISTEN_ADDR` | `:8080` | HTTP listen address |
| `DATABASE_PATH` | `./data/embodied-reading.db` | SQLite database path |
| `TENANT_ID` | `embodied-reading` | Bootstrap tenant ID |
| `TENANT_NAME` | `Embodied Reading Studio` | Bootstrap tenant name |
| `BOOTSTRAP_EMAIL` | `coordinator@example.test` | Initial coordinator login |
| `BOOTSTRAP_PASSWORD` | local development value | Initial coordinator password |
| `SESSION_TTL` | `12h` | Absolute session lifetime |
| `SESSION_TOUCH_INTERVAL` | `5m` | Minimum last-seen write interval |
| `WORKER_POLL_INTERVAL` | `2s` | Worker scan interval |
| `WORKER_LEASE_TTL` | `30s` | Worker and reading lease lifetime |
| `WORKER_BATCH_SIZE` | `25` | Maintenance batch limit |
| `SHUTDOWN_TIMEOUT` | `15s` | Graceful shutdown deadline |
| `LOG_LEVEL` | `info` | Structured log threshold |

## Run Locally

Go 1.26 is required. The module pins its language version and verification uses the locally installed toolchain.

```text
GOTOOLCHAIN=local go mod download
GOTOOLCHAIN=local go run ./cmd/server
```

Then check:

```text
curl http://127.0.0.1:8080/health/live
curl http://127.0.0.1:8080/health/ready
```

The first startup creates the database and bootstrap coordinator. Reusing the same database is supported and does not recreate the coordinator or replay the schema.

## Verification

The baseline verification commands are:

```text
GOTOOLCHAIN=local go test ./... -count=1
GOTOOLCHAIN=local go test -race ./... -count=1
GOTOOLCHAIN=local go vet ./...
GOTOOLCHAIN=local go build ./...
```

Tests cover aggregate state machines, illegal transitions, tenant isolation, optimistic conflicts, transaction rollback, real SQLite migrations, restart recovery, deterministic concurrent outbox claim, context cancellation, HTTP request IDs and error mapping, and worker shutdown.

## Docker

The root `Dockerfile` is a multi-stage build using the Go version required by `go.mod`. It does not pin a CPU architecture and builds the server from `./cmd/server`.

```text
docker build -t embodied-reading-studio:local .
docker run --rm -p 8080:8080 -e BOOTSTRAP_PASSWORD='replace-with-a-long-password' embodied-reading-studio:local
```

The runtime image runs as an unprivileged user and persists SQLite data under `/data`.
