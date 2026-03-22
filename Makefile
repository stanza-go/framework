.PHONY: test bench vet lint check

test:
	CGO_ENABLED=1 go test -race -count=1 ./pkg/...

bench:
	CGO_ENABLED=1 go test -bench=. -benchmem -run=^$$ ./pkg/...

vet:
	CGO_ENABLED=1 go vet ./...

lint:
	CGO_ENABLED=1 golangci-lint run ./pkg/...

check: vet lint test
