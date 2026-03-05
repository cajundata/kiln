# ---- Config ----
KILN := ./kiln
PROMPT_DIR := .kiln/prompts
TASKS_FILE := .kiln/tasks.yaml
TARGETS_FILE := .kiln/targets.mk

# ---- Generated targets (conditionally included) ----
-include $(TARGETS_FILE)

# ---- Phony Targets ----
.PHONY: plan graph clean

# Generate tasks.yaml from PRD.md
plan:
	$(KILN) plan

# Generate Make targets from tasks.yaml
graph:
	$(KILN) gen-make \
		--tasks $(TASKS_FILE) \
		--out $(TARGETS_FILE)

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