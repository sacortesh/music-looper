# Task Spec: FFT dependency (go-dsp not in go.mod; BPM/FFT analysis is heuristic-only)

Slug: fft-dependency--go-dsp-not-in-go-mod--bpm-fft-anal
Branch: task/fft-dependency--go-dsp-not-in-go-mod--bpm-fft-anal
Created: 2026-04-26

---

## BDD Acceptance Criteria

Feature: FFT dependency integration for BPM and spectral analysis

  Scenario: go-dsp added to go.mod
    Given the project has no go-dsp entry in go.mod
    When the developer runs `go get github.com/madelynnblue/go-dsp/fft`
    Then go.mod and go.sum contain a resolved entry for `github.com/madelynnblue/go-dsp`

  Scenario: BPM computed via FFT on onset envelope
    Given a decoded mono signal at 11025 Hz
    When BPM analysis runs using the FFT-based onset-flux method
    Then the returned BPM is a float64 in the range [40.0, 240.0] and is not the heuristic fallback constant

  Scenario: FFT result used in TrackAnalysis FocusScore
    Given an analyzed track with FFT-derived BPM ~75
    When the analyze/score mode produces a TrackAnalysis struct
    Then FocusScore is calculated using the real BPM field and not a placeholder zero value

  Scenario: Heuristic fallback when FFT returns invalid result
    Given a mono signal that is silent or too short for FFT windowing
    When BPM analysis is called
    Then the system returns a safe fallback BPM of 0.0 and logs a warning without panicking

---



## Notes

<!-- Human notes appended here during execute-task.sh iterations -->
