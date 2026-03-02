# ---- Config ----
KILN := ./kiln
PROMPT_DIR := .kiln/prompts
TASKS_FILE := .kiln/tasks.yaml
TARGETS_FILE := .kiln/targets.mk

# ---- Phony Targets ----
.PHONY: plan graph all

# Generate tasks.yaml from PRD.md
plan:
	$(KILN) exec \
		--task-id extract-tasks \
		--prompt-file $(PROMPT_DIR)/00_extract_tasks.md

# Generate Make targets from tasks.yaml
graph:
	$(KILN) gen-make \
		--tasks $(TASKS_FILE) \
		--out $(TARGETS_FILE)

# Run all tasks in graph
all: graph
	$(MAKE) -f $(TARGETS_FILE) all

# Include generated targets if present
-include $(TARGETS_FILE)