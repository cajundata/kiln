.PHONY: all
all: .kiln/done/validation-hooks.done .kiln/done/verify-plan.done .kiln/done/prompt-chaining.done

.kiln/done/validation-hooks.done: .kiln/done/richer-schema.done
	$(KILN) exec --task-id validation-hooks --timeout 90m

.kiln/done/verify-plan.done: .kiln/done/richer-schema.done .kiln/done/validation-hooks.done
	$(KILN) exec --task-id verify-plan --timeout 90m

.kiln/done/prompt-chaining.done: .kiln/done/unify-closure.done .kiln/done/richer-schema.done
	$(KILN) exec --task-id prompt-chaining

