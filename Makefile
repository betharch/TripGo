-include .env
export

BIN            := bin/trip-service
IMAGE          := trip-service:dev
MIGRATIONS_DIR := migrations
COVERPKG       := ./internal/domain/...,./internal/usecase/...
COVERPROFILE   := coverage.out

.PHONY: build run test test-integration lint generate clean \
	migrate migrate-down migrate-reset migrate-status migration check-db-url \
	docker-build docker-run

build:
	go build -o $(BIN) ./cmd/service

run:
	go run ./cmd/service

test:
	go test -race -count=1 -coverpkg=$(COVERPKG) -coverprofile=$(COVERPROFILE) ./...
	go tool cover -func=$(COVERPROFILE) | tail -n 1

test-integration: check-db-url
	go test -race -count=1 -tags=integration ./internal/repository/...

lint:
	golangci-lint run ./...

generate:
	go generate ./...

clean:
	rm -rf bin $(COVERPROFILE)

migrate: check-db-url
	go tool goose -dir $(MIGRATIONS_DIR) postgres "$$DATABASE_URL" up

migrate-down: check-db-url
	go tool goose -dir $(MIGRATIONS_DIR) postgres "$$DATABASE_URL" down

migrate-reset: check-db-url
	go tool goose -dir $(MIGRATIONS_DIR) postgres "$$DATABASE_URL" reset

migrate-status: check-db-url
	go tool goose -dir $(MIGRATIONS_DIR) postgres "$$DATABASE_URL" status

migration:
	@test -n "$(name)"
	go tool goose -dir $(MIGRATIONS_DIR) -s create $(name) sql

check-db-url:
	@test -n "$$DATABASE_URL"

docker-build:
	env -u OTEL_EXPORTER_OTLP_ENDPOINT docker build -f deploy/Dockerfile -t $(IMAGE) .

docker-run: docker-build
	env -u OTEL_EXPORTER_OTLP_ENDPOINT IMAGE=$(IMAGE) sh deploy/docker-run.sh
