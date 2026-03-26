FROM golang:1.25-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/external-event-projector ./cmd/external-event-projector
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/webhook-producer ./cmd/webhook-producer
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/webhook-worker ./cmd/webhook-worker
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/indexer ./cmd/indexer
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/teraslack-mcp-server ./cmd/teraslack-mcp-server

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=build /out/server /app/server
COPY --from=build /out/external-event-projector /app/external-event-projector
COPY --from=build /out/webhook-producer /app/webhook-producer
COPY --from=build /out/webhook-worker /app/webhook-worker
COPY --from=build /out/indexer /app/indexer
COPY --from=build /out/teraslack-mcp-server /app/teraslack-mcp-server
COPY --from=build /src/internal/repository/migrations /app/internal/repository/migrations
COPY docker/start.sh /app/start

RUN chmod +x /app/start

EXPOSE 8080

CMD ["/app/start"]
