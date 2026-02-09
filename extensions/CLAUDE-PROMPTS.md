# Claude Prompts for Extension Development

Copy-paste prompts for building a new Go Composer extension with Claude. Follow these in order. Each prompt builds on the previous phase.

Replace `[EXTENSION_NAME]` and `[DESCRIPTION]` with your values throughout.

---

## Prompt 1 — Discovery and Design

Start every new extension here. Claude reads the codebase and proposes an approach.

```
Read through the following folders to understand the extension development patterns:
- /path/to/built-on-envoy/extensions/example-go/
- /path/to/built-on-envoy/extensions/internal/goplugin/
- /path/to/built-on-envoy/extensions/soap-rest/soap-rest/ (as a reference implementation)

I want to create a new Go extension called [EXTENSION_NAME] in
/path/to/built-on-envoy/extensions/[EXTENSION_NAME]/[EXTENSION_NAME]/.

This extension should: [DESCRIPTION — be specific about inputs, outputs, behavior,
and any protocols or formats involved]

Analyze and let me know:
1. Your proposed approach and architecture
2. Key design decisions and trade-offs
3. Any questions before you start

Do not generate any code until I confirm. Explain your thinking and approach.
```

**What to look for in Claude's response:**
- Does it understand the Composer factory pattern (`ConfigFactory` → `FilterFactory` → `Filter`)?
- Does it correctly plan body buffering if needed (`HeadersStatusStop` + `BodyStatusStopAndBuffer`)?
- Are the config structures reasonable?
- Are there design choices you need to make? Make them explicitly in the next prompt.

---

## Prompt 2 — Design Confirmation

Review Claude's analysis and confirm decisions. Be explicit.

```
Go with [YOUR CHOICES — e.g., "generic mapping, bidirectional, JSON config"].

[Any additional requirements — e.g., "Also support the reverse direction",
"Use only Go standard library", "Handle error cases with proper HTTP status codes"]

Proceed with generating the code.
```

---

## Prompt 3 — Code Generation

If you didn't include "proceed with generating the code" in Prompt 2:

```
I've confirmed the design. Generate the code.

Create:
1. plugin.go — Full implementation
2. manifest.yaml — Extension metadata

Follow the patterns from example-go for the factory pattern and entry point.
Use only Go standard library beyond the Envoy SDK.
```

**After this prompt:** Run `go build -buildmode=plugin -o [EXTENSION_NAME].so .` to verify it compiles.

---

## Prompt 4 — Build and Run Infrastructure

```
Create the build and run infrastructure:

1. A Makefile with build, install, and clean targets
   (follow the pattern from the soap-rest extension)
2. A buildandrun.sh script that builds, installs, and runs boe with a
   test configuration
3. Make sure BOE_BIN is configurable and auto-detected from the project root
```

**After this prompt:** Run `bash buildandrun.sh` to verify the extension starts.

---

## Prompt 5 — Integration Tests

```
Create a test.sh script with integration tests for the extension.

Include:
- A connectivity check before running tests
- Colored pass/fail output with test numbering
- Support for -v (verbose) and -t N (run specific test)
- Test categories:
  - Happy path (normal operation)
  - Error handling (malformed input, missing fields)
  - Edge cases (empty body, large payload, special characters)
  - Passthrough (requests that should NOT be transformed)

The extension is running on http://localhost:10000 with httpbin.org as upstream.
```

**After this prompt:** Start the extension in one terminal, run `bash test.sh` in another. Fix any failures before moving on.

---

## Prompt 6 — Performance Optimization

```
Check the code for any optimization. Make sure the code is optimized for performance.

Specifically look at:
1. Unnecessary allocations in the request/response path
2. Opportunities to pre-compute at config load time
3. Buffer pre-sizing where sizes are known or estimable
4. fmt.Sprintf usage in hot paths (replace with bytes.Buffer)
5. Singleton patterns for reusable objects (e.g., string replacers)
6. String operations that can use cheaper alternatives

After making changes, verify the build still succeeds and all integration tests pass.
```

**After this prompt:** Run `bash test.sh` to verify nothing broke.

---

## Prompt 7 — Memory Safety Audit

```
Check the code for any memory corruption or safety issues. Specifically look for:

1. Nil pointer dereferences — especially on interface types, optional fields,
   and function return values
2. Index out of bounds — slice/string access without length checks
3. Unescaped user input in structured output (JSON, XML)
4. Operator precedence ambiguity in boolean expressions
5. Resource leaks (unclosed readers, unbounded buffers)

Fix any issues found. Explain what each fix prevents.
```

---

## Prompt 8 — Unit Tests

```
Add unit tests for the code. Create plugin_test.go with:

1. Tests for all pure utility functions (parsing, building, config helpers,
   path matching, string manipulation)
2. Edge cases: empty input, nil values, special characters, deeply nested structures,
   unicode, whitespace
3. Roundtrip tests (transform → reverse transform → verify equality)
4. Tests that verify the safety fixes from the previous step
5. Benchmark tests for hot-path functions

Note: Functions that depend on the Envoy SDK handle (filter callbacks) cannot be
unit tested without mocking. Focus on pure functions.

Also create a rununitperf.sh script that runs unit tests and benchmarks with
colored output. Support --unit, --bench, and --cover flags.
```

**After this prompt:** Run `go test -v ./...` and `bash rununitperf.sh --bench`.

---

## Prompt 9 — Documentation

```
Create a README.md for the extension that includes:

1. Overview — what it does, modes of operation
2. Directory structure
3. Quick start — build, run, and example requests (curl commands)
4. Full configuration reference — all fields with types, defaults, and examples
5. Architecture — mode detection logic, request/response flow (ASCII diagrams),
   key components table
6. Data mapping rules (if applicable)
7. Testing instructions — unit tests, integration tests, benchmarks
8. Performance data — benchmark results table
9. Optimizations applied

Follow the style and depth of the soap-rest README.
```

---

## Optional: Combined Safety + Tests Prompt

If you want to do Prompts 7 and 8 in one shot:

```
Can you also check for any memory corruption issues that can happen.
Also add unit tests for the code that is written.
```

This is what was used for soap-rest. Claude will audit first, fix issues, then write tests that cover both normal functionality and the safety fixes.

---

## Tips

1. **Always start with Prompt 1.** Claude needs to read the existing code to produce consistent output.

2. **Build after every phase.** Don't batch — a compile error in Prompt 3 is cheaper to fix than one discovered in Prompt 8.

3. **Don't skip Prompts 6-7.** The performance and safety phases found real bugs in soap-rest:
   - Nil pointer dereference in `replaceBody` (would panic in production)
   - JSON injection in error responses (invalid JSON from error messages with quotes)
   - 12 performance optimizations reducing allocations in hot paths

4. **Be specific in Prompt 1.** The more detail you give about inputs, outputs, protocols, and edge cases, the better the initial design. Vague descriptions lead to multiple revision rounds.

5. **Make design choices explicit in Prompt 2.** Don't say "whatever you think is best." Say "use generic mapping" or "support both directions." Ambiguity causes assumptions you'll need to undo later.

6. **If Claude asks clarifying questions, answer all of them** before saying "proceed." Unanswered questions become assumptions in the code.
