.PHONY: check fmt vet test build vuln e2e

check: fmt vet test build vuln

fmt:
	test -z "$$(gofmt -l .)"

vet:
	go vet ./...

test:
	go test ./...

build:
	CGO_ENABLED=0 go build -o /dev/null ./cmd/secretsanta

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

e2e:
	cd tests/e2e && npx playwright test
