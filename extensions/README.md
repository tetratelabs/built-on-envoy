# Extensions Directory

This directory will contain community-contributed Envoy extensions.

## Structure

Each extension should be in its own directory with:

```
extension-name/
├── README.md           # Description and usage
├── manifest.yaml       # Metadata (name, version, author, etc.)
├── main.go             # Extension code
├── config.yaml         # Example Envoy configuration
└── examples/           # Example usage and test cases
```

## Contributing an Extension

1. Fork this repository
2. Create a new directory under `extensions/` with your extension name
3. Add all required files (sboe structure above)
4. Ensure your README includes:
   - Description of what the extension does
   - Installation/usage instructions
   - Configuration options
   - Examples
5. Open a pull request

Or use the CLI: `boe plugin publish ./your-extension`

## Extension Guidelines

- Extensions should be focused on a single responsibility
- Include comprehensive documentation
- Provide working examples
- Follow Envoy best practices
- Include tests where applicable

## Coming Soon

Featured extensions:
- `rate-limiter` - Token bucket rate limiting
- `auth-jwt` - JWT authentication
- `cors` - CORS policy management
- `request-logger` - Advanced logging
- `transform-headers` - Header manipulation
- `cache` - HTTP caching

Stay tuned!
