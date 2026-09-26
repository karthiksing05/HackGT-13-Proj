# ------------------------------------------------------------------------------
# Build Stage
# ------------------------------------------------------------------------------
FROM golang:1.26-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /app/bin/sidequestz-server main.go

# ------------------------------------------------------------------------------
# Production Minimal Runner Stage
# ------------------------------------------------------------------------------
FROM alpine:3.20

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata

# Create non-root system user
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

COPY --from=builder /app/bin/sidequestz-server /app/sidequestz-server

USER appuser

EXPOSE 8080

ENV PORT=8080
ENV MONGO_URI="mongodb://127.0.0.1:27017"
ENV MONGO_DB="hackgt"

CMD ["/app/sidequestz-server"]
