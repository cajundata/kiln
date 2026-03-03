.PHONY: all
all: .kiln/done/exec-timeout.done .kiln/done/exec-retry.done .kiln/done/exec-footer.done .kiln/done/exec-logging.done .kiln/done/validate-schema.done .kiln/done/validate-cycles.done .kiln/done/gen-make.done .kiln/done/makefile-setup.done .kiln/done/prompt-extract-tasks.done

.kiln/done/exec-timeout.done:
	$(KILN) exec --task-id exec-timeout --prompt-file .kiln/prompts/tasks/exec-timeout.md && touch $@

.kiln/done/exec-retry.done: .kiln/done/exec-timeout.done
	$(KILN) exec --task-id exec-retry --prompt-file .kiln/prompts/tasks/exec-retry.md && touch $@

.kiln/done/exec-footer.done: .kiln/done/exec-retry.done
	$(KILN) exec --task-id exec-footer --prompt-file .kiln/prompts/tasks/exec-footer.md && touch $@

.kiln/done/exec-logging.done: .kiln/done/exec-retry.done
	$(KILN) exec --task-id exec-logging --prompt-file .kiln/prompts/tasks/exec-logging.md && touch $@

.kiln/done/validate-schema.done:
	$(KILN) exec --task-id validate-schema --prompt-file .kiln/prompts/tasks/validate-schema.md && touch $@

.kiln/done/validate-cycles.done: .kiln/done/validate-schema.done
	$(KILN) exec --task-id validate-cycles --prompt-file .kiln/prompts/tasks/validate-cycles.md && touch $@

.kiln/done/gen-make.done: .kiln/done/validate-cycles.done
	$(KILN) exec --task-id gen-make --prompt-file .kiln/prompts/tasks/gen-make.md && touch $@

.kiln/done/makefile-setup.done:
	$(KILN) exec --task-id makefile-setup --prompt-file .kiln/prompts/tasks/makefile-setup.md && touch $@

.kiln/done/prompt-extract-tasks.done:
	$(KILN) exec --task-id prompt-extract-tasks --prompt-file .kiln/prompts/tasks/prompt-extract-tasks.md && touch $@

