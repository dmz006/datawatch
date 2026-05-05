# Release Notes — v6.3.1 (whisper CPU timeout fix)

Released: 2026-05-03
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.3.1

## Summary

Patch release fixing whisper local-venv transcription `signal: killed` failures on CPU-only hosts.

## Fixed

- Voice-test endpoint had a 30 s context timeout; transcription endpoint had a 60 s timeout. Both shorter than the ~34–42 s that whisper base takes on CPU. Test endpoint timeout raised to **3 min**; transcription endpoint to **5 min**.
- The unused `json` import in the embedded Python script was removed.
- Operator's GT 1030 GPU (compute capability 6.1) is not supported by the installed PyTorch build (requires CC ≥ 7.5), so CPU is the only viable device — the timeout adjustment is the canonical fix for that hardware tier.

## See also

CHANGELOG.md `[6.3.1]` entry.
