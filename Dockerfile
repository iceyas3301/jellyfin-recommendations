FROM golang:1.23-alpine AS builder

RUN apk --no-cache add ca-certificates

WORKDIR /app

COPY go.mod ./
RUN go mod download 2>/dev/null || true
COPY . .
RUN go mod tidy

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/jellyfin-recommender-bin main.go

FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/jellyfin-recommender-bin /jellyfin-recommender

ENTRYPOINT ["/jellyfin-recommender"]
