#!/usr/bin/env bash
#
# Compare vecbench search performance between two branches using the same
# deterministic index. Builds both binaries, creates a deterministic index
# from the first branch, then searches with both.
#
# Usage:
#   ./scripts/vecbench-compare.sh <baseline-branch> <experiment-branch> [dataset]
#
# Example:
#   ./scripts/vecbench-compare.sh rd-rabitq-2 rd-rabitq wiki-cohere-768-100k-angular

set -euo pipefail

BASELINE_BRANCH="${1:?Usage: $0 <baseline-branch> <experiment-branch> [dataset]}"
EXPERIMENT_BRANCH="${2:?Usage: $0 <baseline-branch> <experiment-branch> [dataset]}"
DATASET="${3:-wiki-cohere-768-100k-angular}"

CACHE_DIR="/tmp/vecbench-compare"
ORIGINAL_BRANCH=$(git rev-parse --abbrev-ref HEAD)

cleanup() {
  git checkout "$ORIGINAL_BRANCH" --quiet 2>/dev/null || true
}
trap cleanup EXIT

mkdir -p "$CACHE_DIR"

echo "=== Configuration ==="
echo "Baseline:   $BASELINE_BRANCH"
echo "Experiment: $EXPERIMENT_BRANCH"
echo "Dataset:    $DATASET"
echo "Cache:      $CACHE_DIR"
echo ""

# Step 1: Build baseline binary.
echo "=== Building baseline ($BASELINE_BRANCH) ==="
git checkout "$BASELINE_BRANCH" --quiet
./dev build pkg/cmd/vecbench
cp bin/vecbench "$CACHE_DIR/vecbench-baseline"
echo ""

# Step 2: Build experiment binary.
echo "=== Building experiment ($EXPERIMENT_BRANCH) ==="
git checkout "$EXPERIMENT_BRANCH" --quiet
./dev build pkg/cmd/vecbench
cp bin/vecbench "$CACHE_DIR/vecbench-experiment"
echo ""

# Step 3: Build deterministic index (using baseline binary).
INDEX_FILE="$CACHE_DIR/$DATASET.idx"
if [ -f "$INDEX_FILE" ]; then
  echo "=== Reusing existing index: $INDEX_FILE ==="
else
  echo "=== Building deterministic index (this takes a while with single worker) ==="
  "$CACHE_DIR/vecbench-baseline" \
    --deterministic --memstore --cache-folder "$CACHE_DIR" \
    build "$DATASET"
fi
echo ""

# Step 4: Run search benchmarks back-to-back.
BASELINE_OUT="$CACHE_DIR/results-baseline.txt"
EXPERIMENT_OUT="$CACHE_DIR/results-experiment.txt"

echo "=== Search: baseline ==="
"$CACHE_DIR/vecbench-baseline" \
  --memstore --cache-folder "$CACHE_DIR" \
  search "$DATASET" 2>&1 | tee "$BASELINE_OUT"
echo ""

echo "=== Search: experiment ==="
"$CACHE_DIR/vecbench-experiment" \
  --memstore --cache-folder "$CACHE_DIR" \
  search "$DATASET" 2>&1 | tee "$EXPERIMENT_OUT"
echo ""

echo "=== Results saved to ==="
echo "  Baseline:   $BASELINE_OUT"
echo "  Experiment: $EXPERIMENT_OUT"
