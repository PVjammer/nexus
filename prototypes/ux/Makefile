# Nexus UX prototype. Run `make help`.
SHELL := /bin/bash
ADDR ?= 127.0.0.1:8740
DATA ?= $(HOME)/.local/share/nexus-ux
# Extra roots the file browser / review pane can open (repeat -root name=path).
# Default: the nexus repo's docs, so you can review the design docs in the app.
ROOTS ?= -root docs=../../docs
CORE_FLAGS ?= $(ROOTS)

.PHONY: help deps build web core shell dev app test e2e reset

help:
	@echo "make deps    install web dependencies (npm)"
	@echo "make build   build the web bundle and the core"
	@echo "make app     build + open the native window (starts the core if needed)"
	@echo "make dev     run only the core and print its URL (for a browser or 'bin/nexus' in another pane)"
	@echo "make test    Go unit tests"
	@echo "make e2e     end-to-end checks against a fresh core (no model needed)"
	@echo "make reset   delete prototype data in $(DATA)"
	@echo
	@echo "Model: pi's default (your homelab spark/Qwen3.8-27B). Override: CORE_FLAGS='-model ollama/qwen3-64k $(ROOTS)'"

deps:
	cd web && npm install

web:
	cd web && npm run build

core:
	go build -o bin/nexus-core ./cmd/nexus-core

build: web core

dev: build
	bin/nexus-core -addr $(ADDR) -data $(DATA) $(CORE_FLAGS)

# The native shell needs GTK + WebKitGTK headers:
#   sudo apt install libgtk-3-dev libwebkit2gtk-4.1-dev
shell:
	cd shell && PKG_CONFIG_PATH="$$PWD/pkgconfig:$$PKG_CONFIG_PATH" go build -o ../bin/nexus .

app: build shell
	bin/nexus -addr $(ADDR) -data $(DATA) -- $(CORE_FLAGS)

test:
	go test ./...

e2e: build
	@D=$$(mktemp -d); bin/nexus-core -data $$D -agent none -addr 127.0.0.1:8799 > $$D/core.log 2>&1 & \
	  PID=$$!; for i in $$(seq 30); do curl -sf 127.0.0.1:8799/healthz >/dev/null && break; sleep 0.2; done; \
	  node scripts/e2e.mjs http://127.0.0.1:8799 $$(cat $$D/token) $$D; RC=$$?; kill $$PID; echo "(scratch data left in $$D)"; exit $$RC

reset:
	@test -n "$(DATA)" && test "$(DATA)" != "/" && test "$(DATA)" != "$(HOME)" || { echo "refusing: DATA=$(DATA)"; exit 1; }
	rm -r -- "$(DATA)"
