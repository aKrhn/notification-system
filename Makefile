.PHONY: build run test test-short test-race test-e2e lint swagger \
        migrate-up migrate-down migrate-create \
        docker-up docker-down docker-build

# ---- Build & Run ----

build:
	go build -o bin/server ./cmd/server

run: build
	./bin/server

# ---- Testing ----

test:
	go test ./...

test-short:
	go test -short ./...

test-race:
	go test -race ./...

test-e2e:
	./scripts/e2e_test.sh

# ---- Linting ----

lint:
	golangci-lint run ./...

# ---- Swagger ----

swagger:
	swag init -g cmd/server/main.go -o docs

# ---- Migrations ----

migrate-up:
	migrate -path migrations -database "$(DATABASE_URL)" up

migrate-down:
	migrate -path migrations -database "$(DATABASE_URL)" down

migrate-create:
	migrate create -ext sql -dir migrations -seq $(name)

# ---- Docker ----

docker-up:
	docker compose up -d

docker-down:
	docker compose down

docker-build:
	docker compose build
