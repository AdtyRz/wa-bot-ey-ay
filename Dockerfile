FROM golang:1.22-alpine AS builder
WORKDIR /app

# Copy go.mod and go.sum first for caching dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the binary (adjust path if your main package differs)
RUN go build -o /app/bin/temanwa-ai ./cmd/bot/main.go

# Runtime image
FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/bin/temanwa-ai /app/temanwa-ai
EXPOSE 8080
ENTRYPOINT ["/app/temanwa-ai"]
