ARG GO_VERSION=1.26

FROM golang:${GO_VERSION}-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/signal-scheduler-bot ./cmd/botsrv

FROM alpine:3.22 AS runner

RUN addgroup -S app && adduser -S app -G app

COPY --from=builder /out/signal-scheduler-bot /usr/local/bin/signal-scheduler-bot

USER app
ENTRYPOINT ["/usr/local/bin/signal-scheduler-bot"]
