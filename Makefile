BINARY := bin/redis-clone

.PHONY: run build test race benchmark clean

run:
	go run ./cmd/server

build:
	go build -o $(BINARY) ./cmd/server

test:
	go test ./...

race:
	go test -race ./...

benchmark:
	go test -run '^$$' -bench . -benchmem ./...

clean:
	rm -rf bin
