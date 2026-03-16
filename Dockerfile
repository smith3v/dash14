FROM golang:1.26 AS builder

WORKDIR /src

ARG TARGETARCH

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY config ./config
COPY pkg ./pkg
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH:-amd64} go build -trimpath -ldflags="-s -w" -o /out/dash14 ./cmd/dash14

FROM alpine:3.20

WORKDIR /app

RUN apk add --no-cache ca-certificates \
	&& addgroup -S dash14 \
	&& adduser -S -D -H -h /app -G dash14 dash14

COPY --from=builder /out/dash14 /usr/local/bin/dash14
COPY templates ./templates

USER dash14

ENTRYPOINT ["/usr/local/bin/dash14"]
CMD ["--config", "/config/config.yaml"]
