.PHONY: dev console-dev console-build test lint migrate seed docker-up docker-down metrics-up check

dev:
	go run ./cmd/api

console-dev:
	cd console && npm run dev

console-build:
	cd console && npm run build

test:
	go test ./...

lint:
	go vet ./...

migrate:
	go run ./cmd/migrate

seed: migrate

docker-up:
	docker compose up --build

metrics-up:
	docker compose --profile external-metrics up --build

docker-down:
	docker compose down

check: console-build lint test
