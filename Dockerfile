FROM golang:1.22-alpine AS builder
WORKDIR /src
# APP_VERSION 由 CI 从仓库根的 VERSION 文件注入（docker build --build-arg APP_VERSION=$(cat VERSION)），
# 使镜像内二进制的版本号与仓库版本一致，页面左上角角标即为该值。
ARG APP_VERSION=dev
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X main.appVersion=${APP_VERSION}" -o /out/netdrive-sweeper .

FROM alpine:3.20
RUN apk add --no-cache tzdata
ENV TZ=Asia/Shanghai
WORKDIR /app
COPY --from=builder /out/netdrive-sweeper /app/netdrive-sweeper
COPY cd2.proto /app/cd2.proto
RUN mkdir -p /app/data
ENV LISTEN=:5000
ENV CONFIG_PATH=/app/data/config.json
ENV RECORDS_PATH=/app/data/records.jsonl
ENV LOG_PATH=/app/data/clean.log
VOLUME ["/app/data"]
EXPOSE 5000
CMD ["/app/netdrive-sweeper"]