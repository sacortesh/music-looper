# Architecture Rules

<!-- Populated from project analysis — edit to reflect actual rules -->
vision:
- CLI tool that extends MP3 files to any target duration by detecting and looping natural repeat sections
- Designed for instrumental/focus music: intro plays once, main body loops seamlessly
- Target users: producers, gamers, lo-fi listeners, video creators needing long background tracks
- Hit-or-miss because the algorithm scores 60% loop-length / 40% correlation — may sacrifice seam quality for length

tech_stack:
- language: Go 1.26.2
- mp3_decode: github.com/hajimehoshi/go-mp3
- mp3_encode: github.com/viert/go-lame (CGO, requires libmp3lame)
- fft: github.com/madelynnblue/go-dsp/fft (fork of mjibson/go-dsp)
- testing: Go stdlib testing + BDD .feature files
- runtime: macOS (brew lame), single binary

data_model:
- AudioStats: path, sampleRate, channels, duration, rawPCM []byte
- MonoSignal: samples []float64, sampleRate int
- LoopResult: Start, End (time.Duration), Correlation float64, LoopLengthPct float64
- TrackAnalysis: path, BPM, EnergyCV, DynamicRangeDB, ZCR, LoopCorr, LoopLengthPct, FocusScore
- loopOptions: minLoop, maxLoop, crossfade, fadeOut, dryRun, verbose

stated_goals:
- Detect best loop points via energy-envelope + Pearson correlation on 5s windows, 10 quietest end candidates from 80–98% of song
- Extend MP3 to target duration with configurable crossfade (default 50ms) and fade-out (default 2000ms)
- Batch mode: process entire directory, output alongside or to separate dir
- Analyze/score mode: rank tracks 0–10 for focus suitability (BPM∼75, low dynamics, warm ZCR, high loop correlation)
- Dry-run mode: detect loop points without writing output
- Root cause of "hit or miss": 0.6 weight on loop length vs 0.5 correlation fallback threshold — long loops are preferred even when the seam correlation is mediocre

