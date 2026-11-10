BIN     ?= cc-usage
PREFIX  ?= $(HOME)/.local

.PHONY: build test preview install clean
build:
	cargo build --release

# 게이트 — 커밋 전에 늘 돈다(AGENTS.md). clippy 는 테스트 코드도 본다.
test:
	cargo fmt --all --check
	cargo clippy --all-targets -- -D warnings
	cargo test

preview: build
	@sh examples/preview.sh ./target/release/$(BIN)

install: build
	install -d $(PREFIX)/bin
	install -m 0755 target/release/$(BIN) $(PREFIX)/bin/$(BIN)

clean:
	cargo clean
