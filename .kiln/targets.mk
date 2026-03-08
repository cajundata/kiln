.PHONY: all
all: .kiln/done/json-output.done .kiln/done/kiln-init.done .kiln/done/profile-strategy.done

.kiln/done/json-output.done:
	$(KILN) exec --task-id json-output

.kiln/done/kiln-init.done:
	$(KILN) exec --task-id kiln-init

.kiln/done/profile-strategy.done:
	$(KILN) exec --task-id profile-strategy

