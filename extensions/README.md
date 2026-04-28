# Extensions Directory

This directory will contain community-contributed Envoy extensions.

## Structure

Each extension should be in its own directory with:

```
extension-name/
├── README.md           # Description and usage
├── manifest.yaml       # Metadata (name, version, author, etc.)
├── {go,lua,rs}         # Extension code (language-specific)
├── config.schema.json  # (Optional) JSON schema describing the extension configuration
```

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
