APP_NAME := pinn-server
DOCKER_IMAGE := pinn:latest

PWD := $(shell pwd)
TMP_DIR := $(PWD)/tmp
MOCK_DIR := $(PWD)/mock

.PHONY: all build run clean test docker-build docker-run help

all: build

build:
	@echo "Building $(APP_NAME)..."
	@mkdir -p bin
	go build -o bin/$(APP_NAME) cmd/server/main.go

run: build
	@echo "Running locally..."
	TMP_DIR=$(TMP_DIR) MOCK_DIR=$(MOCK_DIR) ./bin/$(APP_NAME)

docker-build:
	@echo "Building Docker image $(DOCKER_IMAGE)..."
	docker build -t $(DOCKER_IMAGE) -f build/Dockerfile .

docker-run:
	@echo "Starting $(APP_NAME) in Docker..."
	@mkdir -p $(TMP_DIR)
	docker run --rm -it \
		--name $(APP_NAME) \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v $(PWD):$(PWD) \
		-e TMP_DIR=$(TMP_DIR) \
		-e MOCK_DIR=$(MOCK_DIR) \
		-p 8080:8080 \
		$(DOCKER_IMAGE)

clean:
	@echo "Cleaning up..."
	rm -rf bin/
	rm -rf tmp/*

help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^##' Makefile | sed -e 's/## //g' -e 's/: /	/g'
