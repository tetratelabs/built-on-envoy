# Extensions Directory

`composer/` contains bundled filters; other extensions live in top-level directories.

## Structure

Each extension should be in its own directory with:

```
extension-name/
├── README.md            # Description and usage
├── manifest.yaml        # Metadata (name, version, author, etc.)
├── {go,lua,rs}          # Extension code (language-specific)
└── config.schema.json   # (Optional) JSON schema describing the extension configuration
```

## Extension e2e tests

See [tests/e2e](./tests/e2e) for end-to-end tests and instructions for adding them.

## Executable manifest examples

Declare `config` and `commands` in `manifest.yaml` to generate `examples[].code`. From the
repository root, run:

```sh
make -C extensions update-examples EXTENSION_PATH=composer/opa
make -C extensions check-examples EXTENSION_PATH=composer/opa
```

Set `EXTENSION_PATH` to the extension's directory relative to `extensions/`, or `composer` for
the bundle. Review generated changes before committing; `check-examples` checks without writing
and is included in `check`.

See [Executable Extension Examples](../website/src/pages/docs/writing-extension-examples.mdx)
for authoring and output elision. The
[generator contract](../cli/tools/gen-examples/README.md) defines implementation invariants.

## Contributing an Extension

Keep each extension focused on one responsibility. Its README must document its purpose,
setup, configuration, and usage examples. Follow the repository's
[contribution guidelines](../CONTRIBUTING.md) for testing and submitting a pull request.
