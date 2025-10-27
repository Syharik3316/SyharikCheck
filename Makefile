SHELL := /bin/zsh

.PHONY: build api agent docker-init image-api image-agent up down logs agent-tar

build:
	go build ./...

api:
	go build -o bin/api ./cmd/api

agent:
	go build -o bin/agent ./cmd/agent

docker-init:
	docker buildx create --use --name aeza-builder || true
	docker buildx inspect --bootstrap

image-api:
	docker buildx build --platform linux/amd64,linux/arm64 -f Dockerfile.api -t aeza/api:local --load .

image-agent:
	docker buildx build --platform linux/amd64,linux/arm64 -f Dockerfile.agent -t aeza/agent:local --load .

agent-tar:
	docker build -f Dockerfile.agent -t aeza-agent:latest .
	docker save aeza-agent:latest -o aeza-agent.tar
	@echo "Set DEST like user@host:/path to scp automatically"
	@[ -n "$$DEST" ] && scp aeza-agent.tar $$DEST || true

up:
	# Build compose services
	docker compose build
	# Always build and export agent image tar into project root
	docker build -f Dockerfile.agent -t aeza-agent:latest .
	docker save aeza-agent:latest -o aeza-agent.tar
	# Start services
	docker compose up -d

down:
	docker compose down -v

logs:
	docker compose logs -f --tail=200


