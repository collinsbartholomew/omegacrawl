FROM golang:1.25-alpine AS builder
RUN apk add --no-cache gcc musl-dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /clone ./cmd/clone

FROM alpine:3.20
RUN apk add --no-cache chromium libstdc++ ca-certificates tzdata
RUN adduser -D -h /home/cloner cloner
WORKDIR /data
COPY --from=builder /clone /usr/local/bin/clone
USER cloner
ENTRYPOINT ["clone"]
