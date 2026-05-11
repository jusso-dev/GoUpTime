FROM golang:1.23-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/uptime-api ./cmd/api \
 && CGO_ENABLED=0 GOOS=linux go build -o /out/uptime-worker ./cmd/worker \
 && CGO_ENABLED=0 GOOS=linux go build -o /out/uptime-migrate ./cmd/migrate

FROM alpine:3.20
WORKDIR /app
RUN apk add --no-cache ca-certificates
COPY --from=build /out/uptime-api /out/uptime-worker /out/uptime-migrate /app/
COPY migrations /app/migrations
EXPOSE 8008 8009

