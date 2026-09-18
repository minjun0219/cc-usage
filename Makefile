BIN     ?= cc-usage
PREFIX  ?= $(HOME)/.local
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
# 설치된 바이너리가 자기 소스가 어디 있는지 알게 한다 — `cc-usage update` 가
# 그 경로에서 pull·install 한다. 설정에 적게 하지 않는 이유는, 빌드한 자리가
# 곧 답이라 사용자가 따로 적어 줄 게 없어서다.
REPO    ?= $(shell pwd)

.PHONY: build test preview install clean
build:
	CGO_ENABLED=0 go build \
		-ldflags "-s -w -X main.version=$(VERSION) -X main.repoPath=$(REPO)" \
		-o bin/$(BIN) ./cmd/cc-usage

test:
	go vet ./...
	go test ./...

preview: build
	@sh examples/preview.sh ./bin/$(BIN)

install: build
	install -d $(PREFIX)/bin
	install -m 0755 bin/$(BIN) $(PREFIX)/bin/$(BIN)

clean:
	rm -rf bin
