# ── Build stage ────────────────────────────────────────────────────────────────
FROM golang:1.26-alpine AS builder

WORKDIR /src

# Cache dependency downloads separately from the build.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 go build \
        -ldflags="-s -w -X main.version=${VERSION}" \
        -o /blacklist \
        .

# ── Final stage ────────────────────────────────────────────────────────────────
FROM alpine:3.23

# Alpine (not scratch) keeps a shell for CI job images; run as non-root.
RUN adduser -D -u 1000 blacklist

COPY --from=builder /blacklist /usr/local/bin/blacklist

USER blacklist

ENTRYPOINT ["/usr/local/bin/blacklist"]
