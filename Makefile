.PHONY: build clean

BINARY := meterlogger
CMD := ./cmd/meterlogger
GIT_SHA := $(shell git rev-parse --short HEAD)
LDFLAGS := -ldflags="-s -w -X main.CommitSHA=$(GIT_SHA) -X main.BuildDate=$(shell date -u '+%Y-%m-%dT%H:%M:%SZ')"
CGO := CGO_ENABLED=0

build:
	mkdir -p out
	$(CGO) GOOS=linux  GOARCH=amd64 go build $(LDFLAGS) -o out/$(BINARY)-linux-amd64  $(CMD)
	$(CGO) GOOS=linux  GOARCH=arm64 go build $(LDFLAGS) -o out/$(BINARY)-linux-arm64  $(CMD)
	$(CGO) GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o out/$(BINARY)-darwin-arm64 $(CMD)

clean:
	rm -rf out/
