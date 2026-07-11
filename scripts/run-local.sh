#!/bin/zsh
set -euo pipefail

root_dir=$(cd "$(dirname "$0")/.." && pwd)
binary="$root_dir/rosboard"
config="$root_dir/configs/config.local.yaml"

if [[ ! -x "$binary" ]]; then
  print -u2 "rosboard binary not found: $binary"
  print -u2 "build it first with: go build -o ./rosboard ./cmd/rosboard"
  exit 1
fi

if [[ ! -r "$config" ]]; then
  print -u2 "local config not found or unreadable: $config"
  exit 1
fi

cd "$root_dir"
exec "$binary" -config "$config"
