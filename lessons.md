# Lessons & Decisions

> Claude reads this at the start of every iteration.
> It records key decisions, user feedback, and technical learnings accumulated
> across sessions so context is never lost.

## Decisions
- `go-mp3` always decodes to stereo 16-bit PCM (4 bytes per sample frame). Channels=2 is hardcoded based on library behavior.
- `go-lame` encoder quality set to 2 (near-best quality, reasonable speed). CGO requires `libmp3lame` — installed via `brew install lame` at `/usr/local/Cellar/lame/3.100`.
- Mono downsampling uses nearest-sample decimation (not interpolation) — simple and sufficient for autocorrelation analysis at 11025 Hz.

## Feedback
_No feedback recorded yet._

## Learnings
- Go is installed at `/usr/local/go/bin/go` (v1.26.2) but not on default PATH — needs explicit PATH export.
- There is an mp3 called `sample1.mp3`
- Autocorrelation loop detection uses brute-force O(n²) — works at 11025 Hz downsampled rate but will need FFT optimization (Task 7) for longer tracks.
- Crossfade at loop junctions works by trimming cfLen frames from the output tail, then blending them with the head of the next loop iteration using linear interpolation. This avoids infinite-loop bugs where in-place blending doesn't grow the output.
- `TestEncodeMP3_RoundTrip` times out (30s) when re-decoding lame-encoded output — pre-existing issue, possibly go-mp3 struggling with lame's output format. Updated timeout to 60s to accommodate.
- CLI refactored from positional args to `flag.NewFlagSet` for extensibility. The `run()` function returns an exit code for testability, with `main()` as a thin wrapper.
- Loop detection fallback: when autocorrelation peak is below 0.5, the tool falls back to looping the entire track rather than using a low-quality loop point.
- FFT-based autocorrelation uses `github.com/madelynnblue/go-dsp/fft` (fork of `mjibson/go-dsp`). Signal is zero-padded to next power of 2 (≥2n) to avoid circular correlation artifacts. Normalization uses prefix sums of squares for O(1) per-lag energy computation.
- `TestIntegration_FullPipeline` and `TestEncodeMP3_RoundTrip` are slow (~40-120s) because lame encoding of an ~8 min sample is expensive. Integration test is gated behind `testing.Short()` to keep `go test -short` fast.
- Progress reporting uses a package-level `progressReporter` callback. This global state approach is simple but means tests must defer-reset it to nil after use.
