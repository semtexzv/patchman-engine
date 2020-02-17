#!/bin/env bash
set -euo pipefail
IFS=$'\n'
OUT="patchman-engine.txt"
> "$OUT"
for line in `cat go.sum`
do
  IFS=" " read -r -a row <<< "$line"
  if echo "${row[1]}" | grep "go.mod" -v  > /dev/null; then
    echo "mgmt_services:latest:patch/${row[0]}:${row[1]}" >> "$OUT"
  fi
  IFS=$'\n'
done