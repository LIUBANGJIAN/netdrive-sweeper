FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/netdrive-sweeper .

FROM alpine:3.20
WORKDIR /app
COPY --from=builder /out/netdrive-sweeper /app/netdrive-sweeper
COPY cd2.proto /app/cd2.proto
RUN mkdir -p /app/data
ENV LISTEN=:5000
ENV CONFIG_PATH=/app/data/config.json
ENV CACHE_PATH=/app/data/cache.json
ENV LOG_PATH=/app/data/clean.log
VOLUME ["/app/data"]
EXPOSE 5000
CMD ["/app/netdrive-sweeper"]
