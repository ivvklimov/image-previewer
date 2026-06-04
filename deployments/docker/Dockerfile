# Этап сборки
FROM golang:1.23-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG RELEASE=dev
ARG BUILD_DATE
ARG GIT_HASH

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-X github.com/ivvklimov/image-previewer/internal/version.Release=${RELEASE} \
-X github.com/ivvklimov/image-previewer/internal/version.BuildDate=${BUILD_DATE} \
-X github.com/ivvklimov/image-previewer/internal/version.GitHash=${GIT_HASH}" \
-o bin/image-previewer ./cmd/server

# Финальный образ
FROM alpine:3.20

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/bin/image-previewer .
COPY configs/config.yaml ./config.yaml

RUN mkdir -p /app/cache

EXPOSE 8080

ENTRYPOINT ["./image-previewer", "--config=./config.yaml"]
