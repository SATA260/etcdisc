GO ?= go
COMPOSE ?= docker compose

.PHONY: run test fmt up down integration proto

run:
	$(GO) run ./cmd/etcdisc-server

test:
	$(GO) test ./...

integration:
	ETCDISC_RUN_INTEGRATION=1 $(GO) test ./test/integration/...

fmt:
	$(GO) fmt ./...

proto:
	sh scripts/gen_proto.sh

up:
	$(COMPOSE) -f deployments/docker-compose/docker-compose.yml up --build

down:
	$(COMPOSE) -f deployments/docker-compose/docker-compose.yml down -v
