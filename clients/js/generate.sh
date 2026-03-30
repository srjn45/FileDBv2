#!/usr/bin/env bash
# generate.sh — regenerate proto stubs using grpc-tools and grpc_tools_node_protoc_ts
#
# Requirements (install as devDependencies if needed):
#   npm install --save-dev grpc-tools grpc_tools_node_protoc_ts
#
# Output goes to src/proto/ — commit the results.

set -euo pipefail

PROTO_SRC="$(cd "$(dirname "$0")/../../proto" && pwd)"
DEPS_DIR="$(cd "$(dirname "$0")/proto" && pwd)"
OUT_DIR="$(cd "$(dirname "$0")/src/proto" && pwd)"
PLUGIN="$(cd "$(dirname "$0")/node_modules/.bin" && pwd)/grpc_tools_node_protoc_plugin"
TS_PLUGIN="$(cd "$(dirname "$0")/node_modules/.bin" && pwd)/protoc-gen-ts"

mkdir -p "$OUT_DIR"

npx grpc_tools_node_protoc \
  --js_out=import_style=commonjs,binary:"$OUT_DIR" \
  --grpc_out=grpc_js:"$OUT_DIR" \
  --plugin=protoc-gen-grpc="$PLUGIN" \
  --ts_out=grpc_js:"$OUT_DIR" \
  --plugin=protoc-gen-ts="$TS_PLUGIN" \
  -I "$PROTO_SRC" \
  -I "$DEPS_DIR" \
  "$PROTO_SRC/filedb.proto"

echo "Done. Stubs written to $OUT_DIR"
