# ────────────────────────────────────────────────
# Stage 1 — Build
# ────────────────────────────────────────────────
FROM golang:1.25-alpine AS builder

# Install git (needed for go modules that resolve via git)
RUN apk add --no-cache git ca-certificates

WORKDIR /app

# Download dependencies first (layer-cached unless go.mod/go.sum changes)
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build a fully static binary
COPY . .
RUN CGO_ENABLED=0 GOOS=linux \
    go build -ldflags="-w -s" -o /app/pr-reviewer-ai .

# ────────────────────────────────────────────────
# Stage 2 — Run
# ────────────────────────────────────────────────
FROM alpine:3.19

# ca-certificates needed for TLS calls to GitLab / Gemini APIs
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/pr-reviewer-ai .

# The app reads all config from environment variables — no config files to COPY.
EXPOSE 8080

HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:8080/healthz || exit 1

ENTRYPOINT ["./pr-reviewer-ai"]
