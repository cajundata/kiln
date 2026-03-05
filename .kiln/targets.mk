.PHONY: all
all: .kiln/done/state-resumability.done .kiln/done/richer-task-schema.done

.kiln/done/state-resumability.done:
	$(KILN) exec --task-id state-resumability

.kiln/done/richer-task-schema.done:
	$(KILN) exec --task-id richer-task-schema

