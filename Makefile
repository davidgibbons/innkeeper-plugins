# Plugin folders, from index.toml.
PLUGINS := $(shell sed -n 's/^path = "\(.*\)"/\1/p' index.toml)

.PHONY: build check install

# Each binary goes in its plugin's folder, where command = ["./<folder>"] finds it.
build:
	for p in $(PLUGINS); do CGO_ENABLED=0 go build -trimpath -o $$p/$$p ./$$p || exit 1; done

# Runs the contract kit from the core version go.mod pins.
check: build
	for p in $(PLUGINS); do go tool innkeeper plugin check $$p || exit 1; done

# Copies each plugin's binary and manifest into DEST, such as an image's plugin folder.
install: build
	test -n "$(DEST)"
	for p in $(PLUGINS); do mkdir -p $(DEST)/$$p && cp $$p/$$p $$p/plugin.toml $(DEST)/$$p/ || exit 1; done
