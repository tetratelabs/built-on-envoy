# Extensions Directory

This directory will contain community-contributed Envoy extensions.

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

Extension e2e tests live in the [tests/e2e](./tests/e2e) directory. Take a look at the README file
there for details on writing end-to-end tests for your extension.

## Contributing an Extension

1. Fork this repository
2. Create a new directory under `extensions/` with your extension name
3. Add all required files (see structure above)
4. Ensure your README includes:
   - Description of what the extension does
   - Installation/usage instructions
   - Configuration options
   - Examples
5. Open a pull request

## Extension Guidelines

- Extensions should be focused on a single responsibility
- Include comprehensive documentation
- Provide working examples
- Follow Envoy best practices
- Include tests where applicable
