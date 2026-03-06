.PHONY: all
all: .kiln/done/state-resumability.done .kiln/done/richer-schema.done .kiln/done/concurrency-safety.done

.kiln/done/state-resumability.done:
	$(KILN) exec --task-id state-resumability

.kiln/done/richer-schema.done:
	$(KILN) exec --task-id richer-schema

.kiln/done/concurrency-safety.done:
	$(KILN) exec --task-id concurrency-safety

