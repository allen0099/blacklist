# ── Build stage ────────────────────────────────────────────────────────────────
FROM golang:1.26-alpine AS builder

WORKDIR /src

# Cache dependency downloads separately from the build.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build \
        -ldflags="-s -w" \
        -o /blacklist \
        .

# ── Final stage ────────────────────────────────────────────────────────────────
FROM alpine:3.21

# Install ca-certificates for HTTPS calls (future-proofing) and
# add a non-root user so the image is safe to run in GitLab CI.
RUN apk add --no-cache ca-certificates && \
    adduser -D -u 1000 blacklist

COPY --from=builder /blacklist /usr/local/bin/blacklist

USER blacklist

ENTRYPOINT ["/usr/local/bin/blacklist"]
