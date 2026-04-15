# PRD: A golang script that automatically takes a mp3 file passed as an argument. it must read the audio, detect where the patterns in the music begin repeating, and auto extend time. intended time must be passed as a variable in minutes. the maximum longest loop is the strategy to follow.

## Overview
A golang script that automatically takes a mp3 file passed as an argument. it must read the audio, detect where the patterns in the music begin repeating, and auto extend time. intended time must be passed as a variable in minutes. the maximum longest loop is the strategy to follow.

## Phases & Tasks

### Phase 1: Foundation
- [ ] **Task 1**: CLI skeleton with MP3 decoding to PCM. Set up Go module, accept MP3 path + target duration (minutes) as args. Decode MP3 to raw PCM using `github.com/hajimehoshi/go-mp3`. Print audio stats (sample rate, channels, duration, sample count).
- [ ] **Task 2**: MP3 encoding and passthrough round-trip. Add MP3 encoding via `github.com/viert/go-lame` (requires `libmp3lame`). Decode input then re-encode to output file as a faithful copy.
- [ ] **Task 3**: Convert stereo PCM to mono float64 for analysis. Average L+R channels, normalize to [-1.0, 1.0], downsample to 11025 Hz for cheaper correlation.

### Phase 2: Core Algorithm
- [ ] **Task 4**: Loop detection via autocorrelation. Compute normalized autocorrelation on mono signal. Find the highest correlation peak (min loop 10s, max half-track). Report detected loop start, end, and duration.
- [ ] **Task 5**: Audio extension by loop repetition. Use detected loop to extend audio to target duration. Repeat loop body N times with short crossfade (50-100ms) at junctions. Encode and write output MP3.

### Phase 3: Polish
- [ ] **Task 6**: Edge cases, validation, and CLI polish. Input validation, fallback when no loop detected (loop entire track), configurable flags (--output, --min-loop, --max-loop, --crossfade, --dry-run).
- [ ] **Task 7**: Performance optimization. FFT-based cross-correlation via `github.com/mjibson/go-dsp/fft`, progress indicator, --verbose flag, integration test.

