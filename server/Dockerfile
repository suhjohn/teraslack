FROM golang:1.24 AS build

WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/external-event-projector ./cmd/external-event-projector
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/webhook-producer ./cmd/webhook-producer
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/webhook-worker ./cmd/webhook-worker
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/integration-cleanup ./cmd/integration-cleanup

FROM alpine:3.21

RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=build /out/server /app/server
COPY --from=build /out/external-event-projector /app/external-event-projector
COPY --from=build /out/webhook-producer /app/webhook-producer
COPY --from=build /out/webhook-worker /app/webhook-worker
COPY --from=build /out/integration-cleanup /app/integration-cleanup

ENV APP_ROLE=server

CMD ["/bin/sh", "-lc", "exec /app/${APP_ROLE}"]
