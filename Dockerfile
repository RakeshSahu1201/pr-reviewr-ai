# ────────────────────────────────────────────────
# Stage 1 — Build
# ────────────────────────────────────────────────
FROM golang:1.25-alpine AS builder

# Install git (needed for go modules that resolve via git)
RUN apk add --no-cache git ca-certificates

WORKDIR /app

# Download dependencies first (layer-cached unless go.mod/go.sum changes)
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy source, generate ent code, and build binaries
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go generate ./ent
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/api ./cmd/api
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/worker ./cmd/worker

# ────────────────────────────────────────────────
# Stage 2 — Run
# ────────────────────────────────────────────────
FROM alpine:3.19

# ca-certificates needed for TLS calls to GitLab / Gemini APIs
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/api .
COPY --from=builder /app/worker .

# Default to api, but usually overridden by docker-compose command
ENTRYPOINT ["./api"]
