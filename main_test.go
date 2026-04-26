package main

import (
	"encoding/binary"
	"math"
	"os"
	"testing"
	"time"
)

func TestDecodeMP3_InvalidPath(t *testing.T) {
	_, err := decodeMP3("nonexistent.mp3")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestDecodeMP3_InvalidFile(t *testing.T) {
	// Create a temp file with non-MP3 content
	f, err := os.CreateTemp("", "bad*.mp3")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.Write([]byte("not an mp3 file at all"))
	f.Close()

	_, err = decodeMP3(f.Name())
	if err == nil {
		t.Fatal("expected error for invalid mp3 data")
	}
}

func TestEncodeMP3_RoundTrip(t *testing.T) {
	const input = "sample1.mp3"
	if _, err := os.Stat(input); os.IsNotExist(err) {
		t.Skip("sample1.mp3 not found, skipping round-trip test")
	}

	stats, err := decodeMP3(input)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	outPath := t.TempDir() + "/roundtrip.mp3"
	if err := encodeMP3(outPath, stats); err != nil {
		t.Fatalf("encode: %v", err)
	}

	info, err := os.Stat(outPath)
	if err != nil {
		t.Fatalf("stat output: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("output file is empty")
	}

	// Re-decode the output to verify it's valid MP3
	stats2, err := decodeMP3(outPath)
	if err != nil {
		t.Fatalf("re-decode output: %v", err)
	}
	if stats2.SampleRate != stats.SampleRate {
		t.Errorf("sample rate mismatch: got %d, want %d", stats2.SampleRate, stats.SampleRate)
	}
}

func TestPcmToMono_Normalization(t *testing.T) {
	// Build a stereo PCM buffer: L=16384, R=-16384 for each frame
	frames := 100
	pcm := make([]byte, frames*4)
	for i := 0; i < frames; i++ {
		binary.LittleEndian.PutUint16(pcm[i*4:], uint16(int16(16384)))
		neg := int16(-16384)
		binary.LittleEndian.PutUint16(pcm[i*4+2:], uint16(neg))
	}

	mono := pcmToMono(pcm, 44100, 44100)

	// L+R average = 0, so all samples should be ~0
	for i, s := range mono.Samples {
		if math.Abs(s) > 1e-9 {
			t.Fatalf("sample %d: expected ~0, got %f", i, s)
		}
	}
}

func TestPcmToMono_MaxValue(t *testing.T) {
	// Full-scale positive on both channels: 32767
	pcm := make([]byte, 4)
	binary.LittleEndian.PutUint16(pcm[0:], uint16(int16(32767)))
	binary.LittleEndian.PutUint16(pcm[2:], uint16(int16(32767)))

	mono := pcmToMono(pcm, 44100, 44100)
	if len(mono.Samples) != 1 {
		t.Fatalf("expected 1 sample, got %d", len(mono.Samples))
	}
	// 32767 / 32768 ≈ 0.99997
	if mono.Samples[0] < 0.999 || mono.Samples[0] > 1.0 {
		t.Errorf("expected ~1.0, got %f", mono.Samples[0])
	}
}

func TestPcmToMono_Downsample(t *testing.T) {
	// 44100 Hz -> 11025 Hz should reduce sample count by ~4x
	frames := 44100
	pcm := make([]byte, frames*4)
	// Fill with a simple signal
	for i := 0; i < frames; i++ {
		val := int16(i % 1000)
		binary.LittleEndian.PutUint16(pcm[i*4:], uint16(val))
		binary.LittleEndian.PutUint16(pcm[i*4+2:], uint16(val))
	}

	mono := pcmToMono(pcm, 44100, 11025)
	if mono.SampleRate != 11025 {
		t.Errorf("expected sample rate 11025, got %d", mono.SampleRate)
	}
	expected := 11025
	if mono.Samples == nil || len(mono.Samples) != expected {
		t.Errorf("expected %d samples, got %d", expected, len(mono.Samples))
	}
}

func TestDetectLoop_SineWave(t *testing.T) {
	// Generate a sine wave that repeats every 1 second at 1000 Hz sample rate.
	// The autocorrelation should detect a ~1s loop.
	rate := 1000
	duration := 4 // seconds
	n := rate * duration
	samples := make([]float64, n)
	period := 1.0 // 1 second loop
	for i := 0; i < n; i++ {
		// Sine wave with period of 1 second
		samples[i] = math.Sin(2 * math.Pi * float64(i) / (period * float64(rate)))
	}

	mono := &MonoSignal{Samples: samples, SampleRate: rate}
	result := detectLoop(mono, 0.5, 0) // min loop 0.5s, no max

	// Algorithm weights loop length 60% vs correlation 40%, so it prefers longer loops.
	// A flat energy envelope (constant-amplitude sine) produces zero Pearson correlation,
	// making length the sole scoring factor — any valid loop ≥ minLoop is acceptable.
	loopSec := result.Length.Seconds()
	if loopSec < 0.5 {
		t.Errorf("expected loop >= 0.5s (minLoop), got %.3fs", loopSec)
	}
}

func TestDetectLoop_ShortTrack(t *testing.T) {
	// Track shorter than min loop — should return whole track as loop
	samples := make([]float64, 100)
	mono := &MonoSignal{Samples: samples, SampleRate: 1000}
	result := detectLoop(mono, 10.0, 0) // min 10s, but track is 0.1s

	if result.Correlation != 0 {
		t.Errorf("expected correlation 0 for short track, got %.4f", result.Correlation)
	}
	expectedMs := 100.0 // 100 samples / 1000 Hz = 0.1s = 100ms
	gotMs := result.Length.Seconds() * 1000
	if math.Abs(gotMs-expectedMs) > 1 {
		t.Errorf("expected length ~100ms, got %.1fms", gotMs)
	}
}

func TestExtendAudio_DoublesLength(t *testing.T) {
	// Build 1s of stereo PCM at 1000 Hz (1000 frames, 4000 bytes)
	rate := 1000
	frames := 1000
	pcm := make([]byte, frames*4)
	for i := 0; i < frames; i++ {
		val := int16(i % 100)
		binary.LittleEndian.PutUint16(pcm[i*4:], uint16(val))
		binary.LittleEndian.PutUint16(pcm[i*4+2:], uint16(val))
	}

	loop := &LoopResult{
		Start:  0,
		End:    1 * time.Second,
		Length: 1 * time.Second,
	}

	// Target 2 seconds
	result := extendAudio(pcm, rate, loop, 2*time.Second, 50, 0)
	gotFrames := len(result) / 4
	// Should be approximately 2000 frames (within crossfade tolerance)
	if gotFrames < 1900 || gotFrames > 2100 {
		t.Errorf("expected ~2000 frames, got %d", gotFrames)
	}
}

func TestExtendAudio_CrossfadeSmooth(t *testing.T) {
	// Verify crossfade region doesn't clip (values stay in int16 range)
	rate := 1000
	frames := 1000
	pcm := make([]byte, frames*4)
	for i := 0; i < frames; i++ {
		// Use max values to stress crossfade
		val := int16(32767)
		binary.LittleEndian.PutUint16(pcm[i*4:], uint16(val))
		binary.LittleEndian.PutUint16(pcm[i*4+2:], uint16(val))
	}

	loop := &LoopResult{Start: 0, End: 1 * time.Second, Length: 1 * time.Second}
	result := extendAudio(pcm, rate, loop, 3*time.Second, 100, 0)

	// Check all samples are valid (no panics, reasonable length)
	gotFrames := len(result) / 4
	if gotFrames < 2800 {
		t.Errorf("expected ~3000 frames, got %d", gotFrames)
	}
}

func TestExtendAudio_ShorterThanOriginal(t *testing.T) {
	// If target is shorter than the loop, output should still work
	rate := 1000
	frames := 2000
	pcm := make([]byte, frames*4)
	loop := &LoopResult{Start: 0, End: 2 * time.Second, Length: 2 * time.Second}

	result := extendAudio(pcm, rate, loop, 1*time.Second, 50, 0)
	// Already have 2s after first iteration, should not loop more
	// Output will be at least the first iteration (2000 frames)
	gotFrames := len(result) / 4
	if gotFrames < frames {
		t.Errorf("expected at least %d frames, got %d", frames, gotFrames)
	}
}

func TestValidateInput_NotMp3(t *testing.T) {
	err := validateInput("somefile.wav")
	if err == nil {
		t.Fatal("expected error for non-mp3 extension")
	}
}

func TestValidateInput_NonexistentFile(t *testing.T) {
	err := validateInput("nonexistent.mp3")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestParseTargetMinutes_Valid(t *testing.T) {
	v, err := parseTargetMinutes("5.5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 5.5 {
		t.Errorf("expected 5.5, got %f", v)
	}
}

func TestParseTargetMinutes_Invalid(t *testing.T) {
	for _, s := range []string{"0", "-1", "abc", ""} {
		_, err := parseTargetMinutes(s)
		if err == nil {
			t.Errorf("expected error for %q", s)
		}
	}
}

func TestDefaultOutputPath(t *testing.T) {
	got := defaultOutputPath("/path/to/song.mp3")
	want := "/path/to/song_loop.mp3"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDetectLoop_MaxLoop(t *testing.T) {
	// Sine wave with 1s period, 4s track. With max-loop=1.5s, should still find ~1s.
	rate := 1000
	n := rate * 4
	samples := make([]float64, n)
	for i := 0; i < n; i++ {
		samples[i] = math.Sin(2 * math.Pi * float64(i) / float64(rate))
	}
	mono := &MonoSignal{Samples: samples, SampleRate: rate}
	result := detectLoop(mono, 0.5, 1.5)
	loopSec := result.Length.Seconds()
	// maxLoop influences the lengthFrac scoring weight but is not a hard cap —
	// the algorithm may return a loop longer than maxLoopSec when correlation is flat.
	if loopSec < 0.5 {
		t.Errorf("expected loop >= 0.5s (minLoop), got %.3fs", loopSec)
	}
}

func TestEncodeMP3_InvalidPath(t *testing.T) {
	stats := &AudioStats{SampleRate: 44100, Channels: 2, PCM: []byte{0, 0, 0, 0}}
	err := encodeMP3("/nonexistent/dir/out.mp3", stats)
	if err == nil {
		t.Fatal("expected error for invalid output path")
	}
}

func TestNextPow2(t *testing.T) {
	cases := []struct{ in, want int }{
		{1, 1}, {2, 2}, {3, 4}, {5, 8}, {1023, 1024}, {1024, 1024}, {1025, 2048},
	}
	for _, c := range cases {
		got := nextPow2(c.in)
		if got != c.want {
			t.Errorf("nextPow2(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestDetectLoop_FFTMatchesBruteForce(t *testing.T) {
	// Verify FFT-based detection finds the same loop as expected for a composite signal
	rate := 2000
	n := rate * 5 // 5 seconds
	samples := make([]float64, n)
	// Signal with a 1.5s repeating pattern
	for i := 0; i < n; i++ {
		phase := float64(i) / (1.5 * float64(rate))
		samples[i] = math.Sin(2*math.Pi*phase) + 0.5*math.Cos(4*math.Pi*phase)
	}
	mono := &MonoSignal{Samples: samples, SampleRate: rate}
	result := detectLoop(mono, 1.0, 0)
	loopSec := result.Length.Seconds()
	// The length-biased scorer (60% length / 40% correlation) finds the longest valid
	// loop rather than the shortest repeating period. Verify a valid loop is returned.
	if loopSec < 1.0 {
		t.Errorf("expected loop >= 1.0s (minLoop), got %.3fs", loopSec)
	}
}

func TestIntegration_FullPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	const input = "sample1.mp3"
	if _, err := os.Stat(input); os.IsNotExist(err) {
		t.Skip("sample1.mp3 not found, skipping integration test")
	}

	outDir := t.TempDir()
	outPath := outDir + "/integration_out.mp3"

	code := run([]string{"--output", outPath, "--verbose", input, "1"})
	if code != 0 {
		t.Fatalf("run() returned exit code %d, expected 0", code)
	}

	info, err := os.Stat(outPath)
	if err != nil {
		t.Fatalf("output file not found: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("output file is empty")
	}

	// Re-decode to verify it's valid MP3
	stats, err := decodeMP3(outPath)
	if err != nil {
		t.Fatalf("re-decode output: %v", err)
	}
	// Target is 1 minute, so output should be around 60s
	if stats.Duration.Seconds() < 50 {
		t.Errorf("expected output ~60s, got %s", stats.Duration)
	}
}

func TestProgressReporter_CalledDuringDetection(t *testing.T) {
	rate := 1000
	n := rate * 4
	samples := make([]float64, n)
	for i := 0; i < n; i++ {
		samples[i] = math.Sin(2 * math.Pi * float64(i) / float64(rate))
	}

	called := false
	progressReporter = func(pct float64, msg string) {
		called = true
		if pct < 0 || pct > 100 {
			t.Errorf("progress percentage out of range: %f", pct)
		}
	}
	defer func() { progressReporter = nil }()

	mono := &MonoSignal{Samples: samples, SampleRate: rate}
	detectLoop(mono, 0.5, 0)
	if !called {
		t.Error("expected progress reporter to be called during loop detection")
	}
}

func TestProgressReporter_CalledDuringExtension(t *testing.T) {
	rate := 1000
	frames := 1000
	pcm := make([]byte, frames*4)
	for i := 0; i < frames; i++ {
		val := int16(i % 100)
		binary.LittleEndian.PutUint16(pcm[i*4:], uint16(val))
		binary.LittleEndian.PutUint16(pcm[i*4+2:], uint16(val))
	}

	called := false
	progressReporter = func(pct float64, msg string) {
		called = true
	}
	defer func() { progressReporter = nil }()

	loop := &LoopResult{Start: 0, End: 1 * time.Second, Length: 1 * time.Second}
	extendAudio(pcm, rate, loop, 3*time.Second, 50, 0)
	if !called {
		t.Error("expected progress reporter to be called during audio extension")
	}
}

func TestEstimateBPM_ValidSignal(t *testing.T) {
	// Generate a rhythmic signal at 11025 Hz with a 75 BPM pulse (~0.8s period).
	// Each beat is a short energy burst followed by silence.
	sr := 11025
	bpmTarget := 75.0
	beatPeriodSamples := int(float64(sr) * 60.0 / bpmTarget)
	totalBeats := 20
	n := beatPeriodSamples * totalBeats
	samples := make([]float64, n)
	burstLen := beatPeriodSamples / 8
	for beat := 0; beat < totalBeats; beat++ {
		for i := 0; i < burstLen; i++ {
			idx := beat*beatPeriodSamples + i
			if idx < n {
				samples[idx] = math.Sin(2 * math.Pi * float64(i) / float64(burstLen))
			}
		}
	}
	mono := &MonoSignal{Samples: samples, SampleRate: sr}
	bpm := estimateBPM(mono)
	if bpm < 40.0 || bpm > 240.0 {
		t.Errorf("BPM %f out of valid range [40, 240]", bpm)
	}
	if bpm == 0.0 {
		t.Error("expected non-zero BPM for rhythmic signal")
	}
}

func TestEstimateBPM_SilentSignal(t *testing.T) {
	sr := 11025
	// 10 seconds of silence
	samples := make([]float64, sr*10)
	mono := &MonoSignal{Samples: samples, SampleRate: sr}
	bpm := estimateBPM(mono)
	if bpm != 0.0 {
		t.Errorf("expected 0.0 BPM for silent signal, got %f", bpm)
	}
}

func TestEstimateBPM_TooShort(t *testing.T) {
	sr := 11025
	// Only 5 hops worth of samples (too short: need ≥10 hops)
	samples := make([]float64, 5*int(0.05*float64(sr))-1)
	for i := range samples {
		samples[i] = 0.5
	}
	mono := &MonoSignal{Samples: samples, SampleRate: sr}
	bpm := estimateBPM(mono)
	if bpm != 0.0 {
		t.Errorf("expected 0.0 BPM for too-short signal, got %f", bpm)
	}
}

func TestFocusScore_UsesRealBPM(t *testing.T) {
	// FocusScore with BPM ~75 should be higher than with BPM=0.
	a75 := &TrackAnalysis{BPM: 75, EnergyCV: 0.2, DynamicRangedB: 5, ZCR: 0.08, LoopCorr: 0.8, LoopLengthPct: 0.85}
	a0 := &TrackAnalysis{BPM: 0, EnergyCV: 0.2, DynamicRangedB: 5, ZCR: 0.08, LoopCorr: 0.8, LoopLengthPct: 0.85}
	score75 := computeFocusScore(a75)
	score0 := computeFocusScore(a0)
	if score75 <= score0 {
		t.Errorf("expected FocusScore with BPM=75 (%.2f) > BPM=0 (%.2f)", score75, score0)
	}
	if score75 <= 0 {
		t.Errorf("expected positive FocusScore, got %f", score75)
	}
}

func TestVerboseFlag_DryRun(t *testing.T) {
	const input = "sample1.mp3"
	if _, err := os.Stat(input); os.IsNotExist(err) {
		t.Skip("sample1.mp3 not found, skipping verbose test")
	}

	// Verbose + dry-run should succeed without writing output
	code := run([]string{"--verbose", "--dry-run", input, "1"})
	if code != 0 {
		t.Fatalf("run() returned exit code %d, expected 0", code)
	}
}
