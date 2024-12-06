CGO_ENABLED ?= 0
GOARCH ?= $(shell dpkg-architecture -q DEB_BUILD_ARCH)
GOOS ?= $(shell dpkg-architecture -q DEB_TARGET_ARCH_OS)
VERSION = $(shell cat VERSION)
COMMIT_HASH = $(shell git rev-parse --short HEAD)

.PHONY: bin
bin:
	CGO_ENABLED=$(CGO_ENABLED) GOOS=linux GOARCH=$(GOARCH) GOARM=$(GOARM) go build -mod=mod -ldflags "-extldflags '-static' -X 'main.version=$(VERSION)-$(COMMIT_HASH)'" -o build/network-discovery cmd/main.go

.PHONY: lint
lint:
	@golangci-lint run ./... --config ../.github/golangci.yaml

.PHONY: test-coverage
test-coverage:
	@mkdir -p .coverage
	@go test -race -cover -json -coverprofile=.coverage/cover.out.tmp ./... | grep -Ev "cmd" | tparse -format=markdown > .coverage/test-report.md
	@cat .coverage/cover.out.tmp | grep -Ev "cmd" > .coverage/cover.out
	@go tool cover -func=.coverage/cover.out | grep total | awk '{print substr($$3, 1, length($$3)-1)}' > .coverage/coverage.txt