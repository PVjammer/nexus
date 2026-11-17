# Nexus desk prototype. Run `make help`.
SHELL := /bin/bash
ADDR ?= 127.0.0.1:8741
DATA ?= $(HOME)/.local/share/nexus-desk
WS := $(HOME)/Workspace
AIP := $(WS)/src/github.com/PVjammer/AI-Projects
# Projects `make seeded` adds up front (skipping onboarding). Nexus only reads them.
SEED ?= -project Homelab=$(WS)/homelab -project "AI Projects=$(AIP)" -project foreman=$(AIP)/projects/foreman \
        -project agent-lockdown=$(AIP)/projects/agent-lockdown -project Nexus=$(AIP)/projects/nexus
CORE_FLAGS ?=

.PHONY: help deps build web core shell dev seeded app test e2e eval-home reset

help:
	@echo "make deps     install web dependencies (npm)"
	@echo "make dev      run the core and print its URL. First run = onboarding with the home agent"
	@echo "make seeded   same, but add Homelab, AI Projects, foreman, agent-lockdown and Nexus up front"
	@echo "make app      build + open the native window (needs GTK/WebKit headers)"
	@echo "make test     Go unit tests"
	@echo "make e2e      end-to-end checks against a fresh core and a scratch project (no model needed)"
	@echo "make eval-home MODEL=spark/Qwen3.6-35B-A3B:low   can this model arrange the home screen? (real model)"
	@echo "make reset    delete prototype data in $(DATA) (your project folders are never touched)"
	@echo
	@echo "Model: pi's default. Override: CORE_FLAGS='-model ollama/qwen3-64k'"
	@echo "Claude Code instead of pi (your Claude Code login, no SDK): CORE_FLAGS='-agent claude' or per kind, e.g. '-agent-task claude -claude-model-task opus'"

deps:
	cd web && npm install

web:
	cd web && npm run build

core:
	go build -o bin/nexus-core ./cmd/nexus-core

build: web core

dev: build
	bin/nexus-core -addr $(ADDR) -data $(DATA) $(CORE_FLAGS)

seeded: build
	bin/nexus-core -addr $(ADDR) -data $(DATA) $(SEED) $(CORE_FLAGS)

# The native shell needs GTK + WebKitGTK headers:
#   sudo apt install libgtk-3-dev libwebkit2gtk-4.1-dev
shell:
	cd shell && PKG_CONFIG_PATH="$$PWD/pkgconfig:$$PKG_CONFIG_PATH" go build -o ../bin/nexus .

app: build shell
	bin/nexus -addr $(ADDR) -data $(DATA) -- $(CORE_FLAGS)

test:
	go test ./...

e2e: build
	@D=$$(mktemp -d); mkdir -p $$D/proj; cp -r testdata/project/. $$D/proj/; printf 'API_KEY=CANARY-ENV-5d1e\n' > $$D/proj/.env; \
	  (cd $$D/proj && git init -q && git add -A && git -c user.name=e2e -c user.email=e2e@local commit -qm init); \
	  bin/nexus-core -data $$D/data -agent none -test-hooks -foreman -addr 127.0.0.1:8798 -scan $$D -project Demo=$$D/proj > $$D/core.log 2>&1 & \
	  PID=$$!; for i in $$(seq 30); do curl -sf 127.0.0.1:8798/healthz >/dev/null && break; sleep 0.2; done; \
	  node scripts/e2e.mjs http://127.0.0.1:8798 $$(cat $$D/data/token) $$D; RC=$$?; kill $$PID; echo "(scratch data left in $$D)"; exit $$RC

# Real-model eval of the home-layout skill + home_* tools. CASES='add-needs undo' runs a subset.
MODEL ?= spark/Qwen3.6-35B-A3B:low
eval-home: build
	node scripts/eval-home.mjs $(MODEL) $(CASES)

reset:
	@test -n "$(DATA)" && test "$(DATA)" != "/" && test "$(DATA)" != "$(HOME)" || { echo "refusing: DATA=$(DATA)"; exit 1; }
	rm -r -- "$(DATA)"
