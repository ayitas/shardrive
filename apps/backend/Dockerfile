# syntax=docker/dockerfile:1
FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/shardrive-api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/shardrive-worker ./cmd/worker

FROM alpine:3.23
RUN apk add --no-cache ca-certificates su-exec && addgroup -S shardrive && adduser -S -G shardrive shardrive
COPY --from=build /out/shardrive-api /shardrive-api
COPY --from=build /out/shardrive-worker /shardrive-worker
COPY --chmod=0755 docker-entrypoint.sh /docker-entrypoint.sh
EXPOSE 8080
ENTRYPOINT ["/docker-entrypoint.sh"]
CMD ["/shardrive-api"]
