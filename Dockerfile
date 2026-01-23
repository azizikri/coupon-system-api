# Stage 1: Builder
FROM golang:1.24-alpine AS builder

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -o api ./cmd/api

# Stage 2: Runner
FROM alpine:latest

# Install ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy binary from builder
COPY --from=builder /build/api .

# Copy migrations directory
COPY --from=builder /build/db/migrations ./db/migrations

# Expose application port
EXPOSE 8080

# Run the binary
CMD ["./api"]
