.PHONY: build run test clean lint air

build:
	go build -o bin/app cmd/main.go

run:
	go run cmd/main.go

test:
	gotestsum --format testname -- -v ./...

clean:
	rm -rf bin/ tmp/

lint:
	golangci-lint run

air:
	air

install-tools:
	go install github.com/cosmtrek/air@latest
	go install gotest.tools/gotestsum@latest
	go install github.com/golang/mock/mockgen@latest
