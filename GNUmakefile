default: fmt lint install generate

LOGLEVEL ?= warn

build:
	go build -v ./...

install: build
	go install -v ./...

lint:
	golangci-lint run

generate:
	cd tools; go generate ./...

fmt:
	gofmt -s -w -e .

test:
	go test -v -cover -timeout=120s -parallel=10 ./...

coverage:
	go test -coverprofile=coverage.out -timeout=120s -parallel=10 ./...
	go tool cover -func=coverage.out
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

testacc:
	TF_ACC=1 TF_LOG=$(LOGLEVEL) go test -v -timeout 120m ./...

testaccnamed:
	TF_ACC=1 TF_LOG=$(LOGLEVEL) go test -run=$(TEST) -v -timeout 120m ./...

.PHONY: fmt lint test testacc build install generate coverage
