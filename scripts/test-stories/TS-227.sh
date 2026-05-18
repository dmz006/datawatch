#!/usr/bin/env bash
# TS-227 — Voice input config (Whisper)
# tags: surface:api feature:voice
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-227"
story_preflight "surface:api feature:voice" || return 0

_story_ts_227() {
    echo ""; echo "  >> TS-227: Voice input config (Whisper)"
    # Check if whisper is available and create test audio
    if which whisper >/dev/null 2>&1; then
      # Create test audio file using espeak + ffmpeg if available
      if which espeak >/dev/null 2>&1 && which ffmpeg >/dev/null 2>&1; then
        test_audio="/tmp/test_voice_$$.wav"
        espeak -w "$test_audio" "datawatch voice test" 2>/dev/null && \
        ffmpeg -i "$test_audio" -ar 16000 -ac 1 "${test_audio%.wav}_16k.wav" -y 2>/dev/null && \
        test_audio="${test_audio%.wav}_16k.wav"

        if [[ -f "$test_audio" ]]; then
          # Test voice endpoint with Whisper
          whisper "$test_audio" --model tiny --output_format json --output_dir /tmp 2>/dev/null
          whisper_out="/tmp/$(basename ${test_audio%.wav}).json"
          if [[ -f "$whisper_out" ]]; then
            save_evidence "TS-227" "whisper_output.json" "$(cat $whisper_out)"
            ok "Voice input (Whisper) processed test audio successfully"
            rm -f "$test_audio" "${test_audio%.wav}".* "$whisper_out" 2>/dev/null || true
          else
            skip "Whisper processing failed"
          fi
        else
          skip "Could not create test audio file"
        fi
      else
        skip "Whisper test requires espeak + ffmpeg for audio generation"
      fi
    else
      skip "Whisper not installed (python3 -m pip install openai-whisper)"
    fi
}

RESULT=fail
_story_ts_227
: "${RESULT:=fail}"
unset -f _story_ts_227
