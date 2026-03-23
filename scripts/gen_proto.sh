#!/bin/sh
set -eu

export PATH="$HOME/go/bin:/usr/local/go/bin:/usr/bin:/bin:$PATH"

protoc -I . \
  --go_out=. \
  --go_opt=paths=source_relative \
  --go-grpc_out=. \
  --go-grpc_opt=paths=source_relative \
  api/proto/etcdisc/v1/etcdisc.proto
