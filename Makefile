.PHONY: test bench vet check

test:
	CGO_ENABLED=1 go test -race -count=1 ./pkg/...

bench:
	CGO_ENABLED=1 go test -bench=. -benchmem -run=^$$ ./pkg/...

vet:
	CGO_ENABLED=1 go vet ./...

check: vet test
