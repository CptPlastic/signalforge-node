.PHONY: dev-server dev-client build-server build-client build lint-server lint-client lint test-server test clean init-env install-up install-down install-logs sync-signalforge-node

SIGNALFORGE_NODE_DIR ?= ../signalforge-node
COMPOSE ?= docker compose

dev-server:
	cd server && go run ./main.go

dev-client:
	cd client && npm run dev

build-server:
	cd server && go build ./...

build-client:
	cd client && npm run build

build: build-server build-client

lint-server:
	cd server && go vet ./...

lint-client:
	cd client && npm run typecheck

lint: lint-server lint-client

test-server:
	cd server && go test ./...

test: test-server

clean:
	rm -rf client/node_modules client/dist

init-env:
	./tools/init-env.sh

install-up:
	$(COMPOSE) --env-file .env.production -f docker-compose.prod.yml up -d

install-down:
	$(COMPOSE) --env-file .env.production -f docker-compose.prod.yml down

install-logs:
	$(COMPOSE) --env-file .env.production -f docker-compose.prod.yml logs -f --tail=100

sync-signalforge-node:
	./tools/sync-signalforge-node.sh . $(SIGNALFORGE_NODE_DIR)
