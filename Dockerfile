FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOTOOLCHAIN=local go build -trimpath -ldflags="-s -w" -o /out/embodied-reading ./cmd/server

FROM alpine:3.22
RUN addgroup -S app && adduser -S -G app app && mkdir -p /data && chown app:app /data
USER app
WORKDIR /app
COPY --from=build /out/embodied-reading /app/embodied-reading
ENV LISTEN_ADDR=:8080 DATABASE_PATH=/data/embodied-reading.db
EXPOSE 8080
ENTRYPOINT ["/app/embodied-reading"]
