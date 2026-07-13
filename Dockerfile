ARG GO_VERSION=1.26
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown

FROM golang:${GO_VERSION}-alpine AS builder
ARG VERSION
ARG COMMIT
ARG BUILD_TIME
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
  -ldflags="-s -w -X 'github.com/demeero/signal-scheduler-bot/cmd/botsrv.Version=${VERSION}' -X 'github.com/demeero/signal-scheduler-bot/cmd/botsrv.Commit=${COMMIT}' -X 'github.com/demeero/signal-scheduler-bot/cmd/botsrv.BuildTime=${BUILD_TIME}'" \
  -o /out/signal-scheduler-bot ./cmd/botsrv

FROM alpine:3.22 AS runner

RUN addgroup -S app && adduser -S app -G app

COPY --from=builder /out/signal-scheduler-bot /usr/local/bin/signal-scheduler-bot

USER app
ENTRYPOINT ["/usr/local/bin/signal-scheduler-bot"]
