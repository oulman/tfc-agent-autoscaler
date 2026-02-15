BINARY    := autoscaler
MODULE    := github.com/oulman/tfc-agent-autoscaler
IMAGE     := tfc-agent-autoscaler
TAG       ?= latest

.PHONY: test build docker lint clean

## test: run all tests with race detector
test:
	go test -race -count=1 ./...

## build: compile the binary
build:
	CGO_ENABLED=0 go build -o $(BINARY) ./cmd/autoscaler/

## docker: build docker image
docker:
	docker build -t $(IMAGE):$(TAG) .

## lint: run go vet
lint:
	go vet ./...

## clean: remove build artifacts
clean:
	rm -f $(BINARY)
