# Task List

## Phase 1 — Foundation
[x] MP3 decode (go-mp3) and encode (go-lame/CGO)
[x] Mono conversion + 4× downsampling (44100→11025 Hz)
[x] CLI flag parsing (all 7 flags + usage)
[x] FFT dependency (go-dsp not in go.mod; BPM/FFT analysis is heuristic-only)



## Phase 2 — Core Loop Engine
[x] Loop detection via energy-envelope + Pearson correlation on 5s windows
[x] 10 quietest end-candidates from 80–98% of song
[x] Audio extension to target duration
[x] Crossfade blending at loop junctions (default 50ms)
[x] Fade-out at end (default 2000ms)
[ ] Seam disguise: tone patch or PS1-style "ong ong" transient layered at loop point



## Phase 3 — Modes & Batch
[x] Dry-run mode (detect only, no write)
[x] Verbose progress output
[x] Batch directory processing with --output dir
[x] Analyze/score mode (BPM, EnergyCV, DynamicRange, ZCR, FocusScore 0–10, markdown table)



## Phase 4 — Testing & Polish
[x] Unit tests (decode, PCM, loop detect, extension, CLI, integration)
[ ] BDD step implementations (6 .feature files exist, no runner/step code)
[ ] performance.feature targets verified (file exists, untested)
[ ] Seam quality metric exposed in --verbose / --analyze output




