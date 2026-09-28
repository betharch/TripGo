-include .env
export

BIN            := bin/trip-service
IMAGE          := trip-service:dev
MIGRATIONS_DIR := migrations

.PHONY: build run test generate clean \
	migrate migrate-down migrate-reset migrate-status migration check-db-url \
	docker-build docker-run

build:
	go build -o $(BIN) ./cmd/trip-service

run:
	go run ./cmd/trip-service

test:
	go test -race ./...

generate:
	go generate ./...

clean:
	rm -rf bin

migrate: check-db-url
	go tool goose -dir $(MIGRATIONS_DIR) postgres "$$DATABASE_URL" up

migrate-down: check-db-url
	go tool goose -dir $(MIGRATIONS_DIR) postgres "$$DATABASE_URL" down

	go tool goose -dir $(MIGRATIONS_DIR) postgres "$$DATABASE_URL" reset

migrate-status: check-db-url
	go tool goose -dir $(MIGRATIONS_DIR) postgres "$$DATABASE_URL" status

migration:
	@test -n "$(name)"
	go tool goose -dir $(MIGRATIONS_DIR) -s create $(name) sql

check-db-url:
	@test -n "$$DATABASE_URL"

docker-build:
	docker build -f deploy/Dockerfile -t $(IMAGE) .

docker-run: docker-build
	IMAGE=$(IMAGE) sh deploy/docker-run.sh
