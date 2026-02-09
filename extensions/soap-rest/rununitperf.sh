#!/usr/bin/env bash
#
# Run all unit tests and benchmark tests for the soap-rest plugin.
#
# Usage:
#   bash rununitperf.sh            # Run unit tests + benchmarks
#   bash rununitperf.sh --unit     # Run only unit tests
#   bash rununitperf.sh --bench    # Run only benchmarks
#   bash rununitperf.sh --cover    # Run unit tests with coverage report
#

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

GREEN='\033[0;32m'
RED='\033[0;31m'
CYAN='\033[0;36m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

MODE="${1:-all}"

run_unit_tests() {
  echo -e "${CYAN}═══════════════════════════════════════════════════════════${NC}"
  echo -e "${CYAN}  Unit Tests${NC}"
  echo -e "${CYAN}═══════════════════════════════════════════════════════════${NC}"
  echo ""

  if go test -v -count=1 ./...; then
    echo ""
    echo -e "${GREEN}✓ All unit tests passed${NC}"
  else
    echo ""
    echo -e "${RED}✗ Some unit tests failed${NC}"
    return 1
  fi
}

run_benchmarks() {
  echo ""
  echo -e "${CYAN}═══════════════════════════════════════════════════════════${NC}"
  echo -e "${CYAN}  Benchmark Tests${NC}"
  echo -e "${CYAN}═══════════════════════════════════════════════════════════${NC}"
  echo ""

  go test -bench=. -benchmem -count=3 -run='^$' ./...

  echo ""
  echo -e "${GREEN}✓ Benchmarks complete${NC}"
}

run_coverage() {
  echo -e "${CYAN}═══════════════════════════════════════════════════════════${NC}"
  echo -e "${CYAN}  Unit Tests with Coverage${NC}"
  echo -e "${CYAN}═══════════════════════════════════════════════════════════${NC}"
  echo ""

  COVER_FILE="coverage.out"

  if go test -v -count=1 -coverprofile="$COVER_FILE" ./...; then
    echo ""
    echo -e "${GREEN}✓ All unit tests passed${NC}"
  else
    echo ""
    echo -e "${RED}✗ Some unit tests failed${NC}"
    return 1
  fi

  echo ""
  echo -e "${YELLOW}Coverage summary:${NC}"
  go tool cover -func="$COVER_FILE"
  echo ""
  echo -e "${CYAN}HTML report: go tool cover -html=${COVER_FILE}${NC}"
}

case "$MODE" in
  --unit)
    run_unit_tests
    ;;
  --bench)
    run_benchmarks
    ;;
  --cover)
    run_coverage
    ;;
  all|*)
    run_unit_tests
    echo ""
    run_benchmarks
    echo ""
    echo -e "${CYAN}═══════════════════════════════════════════════════════════${NC}"
    echo -e "${GREEN}  All done!${NC}"
    echo -e "${CYAN}═══════════════════════════════════════════════════════════${NC}"
    ;;
esac
