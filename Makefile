.PHONY: fmt lint test

fmt:
	gofmt -s -w -e .
	go vet ./...

lint:
	go tool golangci-lint run

test:
	go test -shuffle=on -race -timeout=120s ./...
