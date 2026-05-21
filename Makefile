.RECIPEPREFIX := >
.PHONY: dev-server dev-client build-server build-client build lint-server lint-client lint test-server test clean sync-signalforge-node

SIGNALFORGE_NODE_DIR ?= ../signalforge-node

dev-server:
>cd server && go run ./main.go

dev-client:
>cd client && npm run dev

build-server:
>cd server && go build ./...

build-client:
>cd client && npm run build

build: build-server build-client

lint-server:
>cd server && go vet ./...

lint-client:
>cd client && npm run typecheck

lint: lint-server lint-client

test-server:
>cd server && go test ./...

test: test-server

clean:
>rm -rf client/node_modules client/dist

sync-signalforge-node:
>./tools/sync-signalforge-node.sh . $(SIGNALFORGE_NODE_DIR)
