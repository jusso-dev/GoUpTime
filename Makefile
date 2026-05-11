.PHONY: dev test lint migrate seed docker-up docker-down check

dev:
	go run ./cmd/api

test:
	go test ./...

lint:
	go vet ./...

migrate:
	go run ./cmd/migrate

seed: migrate

docker-up:
	docker compose up --build

docker-down:
	docker compose down

check: lint test

