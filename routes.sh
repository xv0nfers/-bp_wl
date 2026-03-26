#!/bin/bash
set -euo pipefail

gateway="$(ip -o -4 route show to default | awk '/via/ {print $3}' | head -1)"

while read -r remote; do
  remote="${remote#TURN_IP=}"
  remote="${remote#WG_ENDPOINT=}"
  remote="${remote%%:*}"
  [[ -z "$remote" ]] && continue
  [[ "$remote" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]] || continue
  sudo ip r replace "$remote/32" via "$gateway"
done
