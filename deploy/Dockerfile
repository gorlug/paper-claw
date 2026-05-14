# Stage 1: build the static binary.
FROM golang:1.26 AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /paperclaw ./cmd/paperclaw

# Stage 2: minimal runtime image with poppler-utils (pdftotext).
FROM debian:stable-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
        poppler-utils \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /paperclaw /usr/local/bin/paperclaw

# State directory; mount a volume here to persist the SQLite database and
# OAuth token across container restarts.
VOLUME /data

ENTRYPOINT ["paperclaw"]
CMD ["serve", "-config", "/etc/paperclaw/paperclaw.yaml"]
