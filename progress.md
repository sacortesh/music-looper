# Progress

## Completed Tasks
- [x] **Task 1**: CLI skeleton with MP3 decoding to PCM. Go module initialized, accepts MP3 path + target minutes, decodes to PCM via `go-mp3`, prints audio stats.
- [x] **Task 2**: MP3 encoding and passthrough round-trip. Added `go-lame` encoder, decode→re-encode pipeline writes `_loop.mp3` output.
- [x] **Task 3**: Convert stereo PCM to mono float64 for analysis. `pcmToMono()` averages L+R, normalizes to [-1.0, 1.0], downsamples to 11025 Hz.
- [x] **Task 4**: Loop detection via autocorrelation. `detectLoop()` computes normalized autocorrelation, finds best peak between min loop and half-track, reports start/end/length/correlation.
- [x] **Task 5**: Audio extension by loop repetition. `extendAudio()` repeats detected loop body with 50ms crossfade at junctions, encodes extended PCM to output MP3.
- [x] **Task 6**: Edge cases, validation, and CLI polish. Flag-based CLI (`--output`, `--min-loop`, `--max-loop`, `--crossfade`, `--dry-run`), input validation, low-correlation fallback to full-track loop.

## Test Counts
| Type | Count |
|------|-------|
| Unit |  18   |
| BDD  |  16   |
