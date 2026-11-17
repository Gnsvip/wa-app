#!/usr/bin/env sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
PATH="$(go env GOPATH)/bin:$PATH"

rm -rf "$ROOT/gen"
mkdir -p "$ROOT/gen/go"

set -- -I "$ROOT/proto"
if [ -d /usr/include/google/protobuf ]; then
  set -- "$@" -I /usr/include
fi

protoc "$@" \
  --go_out="$ROOT" \
  --go_opt=module=github.com/byte-v-forge/wa-app \
  --go-grpc_out="$ROOT" \
  --go-grpc_opt=module=github.com/byte-v-forge/wa-app \
  $(find "$ROOT/proto" -name '*.proto' | sort)

gofmt -w "$ROOT/gen/go"
