# innkeeper-plugins

Plugins for [Innkeeper](https://github.com/davidgibbons/innkeeper): importers,
catalogs, app targets, codecs, AI actions, enrichers, and login providers.

Each plugin is a folder with a `plugin.toml` manifest. `index.toml` lists every
plugin. The protocol and contract test kit live in the `innkeeper` repo.

## Building and checking

```bash
go env -w GOPRIVATE=github.com/davidgibbons/*
make build                    # each plugin's binary, into its folder
make check                    # the core's contract kit, against each plugin
go run ./internal/indexcheck  # index.toml matches the plugin folders
```

The core version is pinned in `go.mod` as a tool. Move it with
`go get -tool github.com/davidgibbons/innkeeper/cmd/innkeeper@<commit>`;
that sets both the `protocol` package and the contract kit.

## Adding a plugin

1. Make a folder whose `plugin.toml` has `command = ["./<folder>"]`.
2. Add an `index.toml` entry with the same name, version, protocol, and
   capabilities, plus a description.
3. Ignore the built binary in `.gitignore`.
