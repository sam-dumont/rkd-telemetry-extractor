.PHONY: build test lint clean

BINARY=rkd-parser

build:
	go build -o $(BINARY) .

test:
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

lint:
	go vet ./...

clean:
	rm -f $(BINARY) coverage.out
