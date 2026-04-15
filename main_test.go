package main

import (
	"encoding/binary"
	"math"
	"os"
	"testing"
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

func TestEncodeMP3_InvalidPath(t *testing.T) {
	stats := &AudioStats{SampleRate: 44100, Channels: 2, PCM: []byte{0, 0, 0, 0}}
	err := encodeMP3("/nonexistent/dir/out.mp3", stats)
	if err == nil {
		t.Fatal("expected error for invalid output path")
	}
}
