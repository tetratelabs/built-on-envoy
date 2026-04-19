# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

COVERAGE_THRESHOLD ?= 80

.PHONY: format
format:
	@cargo fmt --check

.PHONY: lint
lint:
	@cargo clippy -- -D warnings

.PHONY: build
build:
	@cargo build --release

.PHONY: test
test:
	@cargo test

.PHONY: test-coverage
test-coverage:
	@cargo llvm-cov --lcov --output-path lcov.info --fail-under-lines $(COVERAGE_THRESHOLD)
	@cargo llvm-cov report --summary-only
