.PHONY: all
all: .kiln/done/unify-closure.done .kiln/done/error-taxonomy.done .kiln/done/recovery-ux.done

.kiln/done/unify-closure.done: .kiln/done/state-resumability.done
	$(KILN) exec --task-id unify-closure

.kiln/done/error-taxonomy.done: .kiln/done/state-resumability.done
	$(KILN) exec --task-id error-taxonomy

.kiln/done/recovery-ux.done: .kiln/done/state-resumability.done
	$(KILN) exec --task-id recovery-ux

