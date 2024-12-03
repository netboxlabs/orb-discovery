CGO_ENABLED ?= 0
GOARCH ?= $(shell dpkg-architecture -q DEB_BUILD_ARCH)
GOOS ?= $(shell dpkg-architecture -q DEB_TARGET_ARCH_OS)
VERSION = $(shell cat VERSION)
COMMIT_HASH = $(shell git rev-parse --short HEAD)

bin:
	CGO_ENABLED=$(CGO_ENABLED) GOOS=linux GOARCH=$(GOARCH) GOARM=$(GOARM) go build -mod=mod -ldflags "-extldflags '-static' -X 'github.com/netboxlabs/orb-discovery/network-discovery/server.version=$(VERSION)-$(COMMIT_HASH)'" -o build/network-discovery cmd/main.go