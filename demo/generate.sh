#!/usr/bin/env bash
# generate.sh — synthesize one WAV per voice profile into demo/.
#
# Usage: ./demo/generate.sh
#
# Runs `go run ./cmd/edgefx` for every built-in profile with a fixed English
# test sentence, writing demo/<profile>.wav. Idempotent: re-running simply
# overwrites the outputs. A failing profile (e.g. network hiccup) does not
# stop the run — the script continues and summarizes failures at the end.
set -u

cd "$(dirname "$0")/.." || exit 1

TEXT="This is a test of the edge fx tts voice profiles. One two three, testing."

PROFILES=(
  aifake
  doubledelay
  scifiatmo
  filmai-d3
  filmai-d4
  broadcast
  broadcast-e2
  broadcast-e3
  jarvis
  edith
  ai-modern
)

mkdir -p demo

failures=()
for name in "${PROFILES[@]}"; do
  out="demo/${name}.wav"
  printf '==> %s -> %s\n' "$name" "$out"
  if go run ./cmd/edgefx -profile "$name" -text "$TEXT" -output "$out"; then
    printf '    ok\n'
  else
    printf '    FAILED\n'
    failures+=("$name")
  fi
done

if ((${#failures[@]})); then
  printf '\n%d of %d profile(s) failed: %s\n' "${#failures[@]}" "${#PROFILES[@]}" "${failures[*]}"
  printf 'Re-run ./demo/generate.sh to retry.\n'
  exit 1
fi

printf '\nAll %d profiles generated. Listen to demo/*.wav\n' "${#PROFILES[@]}"
