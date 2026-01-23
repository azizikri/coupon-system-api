# Flash Sale Coupon System

A REST API for flash sale coupons, built with Go and PostgreSQL. Handles concurrent requests without overselling or letting users claim twice.

## Prerequisites

- **Docker Desktop** (or Docker with Docker Compose)

## How to Run

To start the application and the database, run the following command in the project root:

```bash
docker-compose up --build
```

- **Server**: Starts on port `8080`
- **Health Check**: `curl http://localhost:8080/health`

## How to Test

### Automated Tests
The project includes unit and integration tests. Integration tests automatically spin up a temporary PostgreSQL container using Docker.

```bash
go test ./...
```

### Manual Verification
You can use the following `curl` commands to verify the API endpoints:

1. **Create a Coupon**
   ```bash
   curl -X POST http://localhost:8080/api/coupons \
     -H "Content-Type: application/json" \
     -d '{"name":"PROMO_SUPER","amount":100}'
   ```

2. **Claim a Coupon**
   ```bash
   curl -X POST http://localhost:8080/api/coupons/claim \
     -H "Content-Type: application/json" \
     -d '{"user_id":"user_12345","coupon_name":"PROMO_SUPER"}'
   ```

3. **Get Coupon Details**
   ```bash
   curl http://localhost:8080/api/coupons/PROMO_SUPER
   ```

## API Endpoints

### POST /api/coupons
Creates a new coupon with a specified initial stock.

- **Request Body**: `{"name": "PROMO_SUPER", "amount": 100}`
- **Success Response**: `201 Created`
- **Error Responses**:
  - `409 Conflict`: Coupon name already exists
  - `400 Bad Request`: Invalid JSON body

### POST /api/coupons/claim
Claims a coupon for a specific user.

- **Request Body**: `{"user_id": "user_12345", "coupon_name": "PROMO_SUPER"}`
- **Success Response**: `200 OK`
- **Error Responses**:
  - `404 Not Found`: Coupon does not exist
  - `409 Conflict`: User has already claimed this coupon
  - `409 Conflict`: Coupon is sold out (no remaining stock)
  - `400 Bad Request`: Invalid JSON body

### GET /api/coupons/{name}
Retrieves details of a coupon, including the list of users who claimed it.

- **Success Response**: `200 OK`
- **Response Body**:
  ```json
  {
    "name": "PROMO_SUPER",
    "amount": 100,
    "remaining_amount": 99,
    "claimed_by": ["user_12345"]
  }
  ```
- **Error Responses**:
  - `404 Not Found`: Coupon does not exist

## Architecture Notes

### Database Design
The system uses two separate tables in PostgreSQL:
- **`coupons`**: Stores coupon metadata and current stock (`amount`, `remaining_amount`).
- **`claims`**: Stores individual claim records (`user_id`, `coupon_name`).

### Concurrency & Consistency
- **Uniqueness Constraint**: A composite unique constraint on `(user_id, coupon_name)` in the `claims` table prevents a user from claiming the same coupon more than once.
- **Atomic Transaction Flow**: The claim process is wrapped in a database transaction:
  1. **Insert Claim**: Attempts to insert a record into the `claims` table.
  2. **Decrement Stock**: If the insert succeeds, it decrements `remaining_amount` in the `coupons` table using an atomic `UPDATE ... WHERE remaining_amount > 0` query.
  3. **Rollback**: If the stock decrement fails (sold out), the transaction rolls back, ensuring no claim is recorded without stock.
- **Concurrency Safety**: PostgreSQL's ACID properties and atomic updates handle race conditions—no application-level locks needed.

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `APP_PORT` | Port for the HTTP server | `8080` |
| `DB_HOST` | PostgreSQL host | `localhost` |
| `DB_PORT` | PostgreSQL port | `5432` |
| `DB_USER` | PostgreSQL username | `postgres` |
| `DB_PASSWORD` | PostgreSQL password | `postgres` |
| `DB_NAME` | PostgreSQL database name | `coupondb` |
| `DB_SSLMODE` | PostgreSQL SSL mode | `disable` |

## Technology Stack

- **Go 1.24**
- **Chi Router** for routing
- **PostgreSQL 16**
- **sqlc** for generating Go code from SQL
- **pgx/v5** as the PostgreSQL driver
- **Docker Compose** for local dev
