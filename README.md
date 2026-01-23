# Flash Sale Coupon System

A high-performance flash sale coupon system using an event-driven architecture with Kafka and PostgreSQL.

## Prerequisites

- **Docker Desktop** (or Docker with Docker Compose)
- **Go 1.24+** (if running outside Docker)

## Architecture

The system uses an event-driven architecture to handle coupon creation and claims:
1. **HTTP Gateway**: Receives external requests and publishes them to Kafka request topics.
2. **Kafka**: Acts as the message backbone, ensuring reliability and ordering.
3. **Consumer**: Processes request events, interacts with PostgreSQL, and publishes results to reply topics.

### Event Flow
- `coupon.create.req` / `coupon.claim.req` / `coupon.get.req`: Request topics.
- `coupon.reply.{instance_id}`: Reply topics for synchronous HTTP responses.
- `coupon.*.retry` / `coupon.*.dlq`: Reliability topics for handling failures.

## How to Run

To start the complete stack (App, DB, Kafka, Zookeeper):

```bash
docker-compose up --build
```

- **Server**: Starts on port `8080`
- **Health Check**: `curl http://localhost:8080/health`

## How to Test

### Automated Tests
Integration tests spin up a temporary environment with PostgreSQL and Kafka using Docker Compose.

```bash
go test ./...
```

## API Endpoints

All requests should include `Content-Type: application/json`.

### POST /api/coupons
Registers a new coupon into the system.

- **Request Body**: `{"name": "PROMO_SUPER", "amount": 100}`
- **Success Response**: `201 Created`
- **Errors**: `409 Conflict` (duplicate), `400 Bad Request` (invalid body), `500 Internal Server Error`

### POST /api/coupons/claim
Attempts to claim a coupon for a specific user.

- **Request Body**:
  ```json
  {
    "user_id": "user_12345",
    "coupon_name": "PROMO_SUPER"
  }
  ```
- **Success Response**: `200 OK`
- **Errors**: `409 Conflict` (already claimed or sold out), `404 Not Found` (coupon missing), `400 Bad Request`, `500 Internal Server Error`

### GET /api/coupons/{name}
Retrieves coupon details.

- **Response Body**:
  ```json
  {
    "name": "PROMO_SUPER",
    "amount": 100,
    "remaining_amount": 99,
    "claimed_by": ["user_12345"]
  }
  ```

- **Success Response**: `200 OK`
- **Errors**: `404 Not Found`, `500 Internal Server Error`

## Architecture Notes

### Database Design
- Coupons are stored in a `coupons` table.
- Claim history is stored in a separate `claims` table.
- The schema enforces a unique constraint on `(user_id, coupon_name)` to prevent duplicate claims.

### Locking / Consistency Strategy
- Claim flow is wrapped in a database transaction that inserts the claim and decrements stock atomically.
- `INSERT ... ON CONFLICT (user_id, coupon_name) DO NOTHING` prevents double-claims.
- Stock decrement uses `WHERE remaining_amount > 0` to avoid overselling.

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `EVENT_DRIVEN_ENABLED` | Toggle Kafka integration (`true`/`false`) | `true` |
| `KAFKA_BROKERS` | Kafka bootstrap servers | `kafka:9092` |
| `DB_HOST` | PostgreSQL host | `localhost` |
| `DB_PORT` | PostgreSQL port | `5432` |

## Technology Stack

- **Go 1.24**
- **Apache Kafka** (via `franz-go`)
- **PostgreSQL 16**
- **sqlc** & **pgx/v5**
- **Docker Compose**
