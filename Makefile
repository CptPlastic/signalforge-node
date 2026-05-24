.PHONY: dev-server dev-client build-server build-client build-cli build lint-server lint-client lint test-server test-cli test clean init-env install-up install-down install-logs sync-signalforge-node

SIGNALFORGE_NODE_DIR ?= ../signalforge-node
COMPOSE ?= docker compose
CLI_VERSION ?= dev
CLI_COMMIT ?= local
CLI_DATE ?= local
CLI_LDFLAGS := -X github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-cli/internal/buildinfo.Version=$(CLI_VERSION) -X github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-cli/internal/buildinfo.Commit=$(CLI_COMMIT) -X github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-cli/internal/buildinfo.Date=$(CLI_DATE)

dev-server:
	cd server && go run ./main.go

dev-client:
	cd client && npm run dev

build-server:
	cd server && go build ./...

build-client:
	cd client && npm run build

build-cli:
	cd tools/signalforge-cli && go build -ldflags "$(CLI_LDFLAGS)" -o dist/signalforge ./cmd/signalforge

build: build-server build-client build-cli

lint-server:
	cd server && go vet ./...

lint-client:
	cd client && npm run typecheck

lint: lint-server lint-client

test-server:
	cd server && go test ./...

test-cli:
	cd tools/signalforge-cli && go test ./...

test: test-server test-cli

clean:
	rm -rf client/node_modules client/dist tools/signalforge-cli/dist tools/signalforge-cli/signalforge tools/signalforge-cli/signalforge.exe tools/signalforge-cli/sf tools/signalforge-cli/sf.exe

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
