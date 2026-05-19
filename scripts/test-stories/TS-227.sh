#!/usr/bin/env bash
# TS-227 — Voice input config (Whisper)
# tags: surface:api feature:voice
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-227"
story_preflight "surface:api feature:voice" || return 0

_story_ts_227() {
  local venv_python="/home/dmz/.datawatch/.venv/bin/python3"
  if [[ ! -x "$venv_python" ]]; then
    skip "Whisper venv not found at /home/dmz/.datawatch/.venv"
    return
  fi
  if ! "$venv_python" -c "import whisper" 2>/dev/null; then
    skip "whisper not installed in venv (pip install openai-whisper)"
    return
  fi
  if ! command -v ffmpeg >/dev/null 2>&1; then
    skip "ffmpeg not installed (needed to generate test audio)"
    return
  fi

  local test_audio="/tmp/ts227-tone-$$.wav"
  ffmpeg -f lavfi -i "sine=frequency=440:duration=1" -ar 16000 -ac 1 "$test_audio" -y 2>/dev/null
  if [[ ! -f "$test_audio" ]]; then
    skip "could not generate test audio with ffmpeg"
    return
  fi

  local whisper_out
  whisper_out=$("$venv_python" -c "
import whisper, sys
model = whisper.load_model('tiny')
result = model.transcribe(sys.argv[1])
print(result.get('text',''))
" "$test_audio" 2>/dev/null)
  local rc=$?
  rm -f "$test_audio"
  save_evidence TS-227 "transcript.txt" "$whisper_out"
  if [[ $rc -eq 0 ]]; then
    ok "Whisper (tiny) transcribed test audio via venv /home/dmz/.datawatch/.venv"
  else
    ko "whisper transcription failed rc=$rc"
  fi
}

RESULT=fail
_story_ts_227
: "${RESULT:=fail}"
unset -f _story_ts_227
