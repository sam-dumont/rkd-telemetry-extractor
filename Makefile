.PHONY: test test-python test-go lint build clean

test: test-python test-go

test-python:
	cd python && make test

test-go:
	cd go && make test

lint:
	cd python && make lint
	cd go && make lint

build:
	cd go && make build

clean:
	cd python && make clean
	cd go && make clean
