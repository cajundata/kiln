# ---- Config ----
KILN := ./bin/kiln
PROMPT_DIR := .kiln/prompts
TASKS_FILE := .kiln/tasks.yaml
TARGETS_FILE := .kiln/targets.mk

# ---- Auto-build binary when source changes ----
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

$(KILN): cmd/kiln/main.go
	@echo "kiln: rebuilding $(KILN) ($(VERSION))..."
	@go build -ldflags "-X main.version=$(VERSION)" -o ./bin/kiln ./cmd/kiln
	@echo "kiln: build complete"

# ---- Generated targets (conditionally included) ----
-include $(TARGETS_FILE)

# ---- Phony Targets ----
.PHONY: plan graph clean

# Generate tasks.yaml from PRD.md
plan: $(KILN)
	$(KILN) plan

# Generate Make targets from tasks.yaml
graph: $(KILN)
	$(KILN) gen-make \
		--tasks $(TASKS_FILE) \
		--out $(TARGETS_FILE) \
		$(if $(DEV_PHASE),--dev-phase $(DEV_PHASE))

# Fallback: fail with a clear message if targets.mk has not been generated yet
ifeq ($(wildcard $(TARGETS_FILE)),)
.PHONY: all
all:
	$(error $(TARGETS_FILE) not found — run 'make graph' first)
endif

# Remove all generated artifacts
.PHONY: clean
clean:
	rm -rf .kiln/done .kiln/logs $(TARGETS_FILE)