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

	// Should detect a loop close to 1.0 second
	loopSec := result.Length.Seconds()
	if loopSec < 0.9 || loopSec > 1.1 {
		t.Errorf("expected loop ~1.0s, got %.3fs", loopSec)
	}
	if result.Correlation < 0.99 {
		t.Errorf("expected high correlation, got %.4f", result.Correlation)
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
	result := extendAudio(pcm, rate, loop, 2*time.Second, 50)
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
	result := extendAudio(pcm, rate, loop, 3*time.Second, 100)

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

	result := extendAudio(pcm, rate, loop, 1*time.Second, 50)
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
	if loopSec < 0.9 || loopSec > 1.6 {
		t.Errorf("expected loop ~1.0s (max 1.5s), got %.3fs", loopSec)
	}
}

func TestEncodeMP3_InvalidPath(t *testing.T) {
	stats := &AudioStats{SampleRate: 44100, Channels: 2, PCM: []byte{0, 0, 0, 0}}
	err := encodeMP3("/nonexistent/dir/out.mp3", stats)
	if err == nil {
		t.Fatal("expected error for invalid output path")
	}
}
