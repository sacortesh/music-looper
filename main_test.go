package main

import (
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

func TestEncodeMP3_InvalidPath(t *testing.T) {
	stats := &AudioStats{SampleRate: 44100, Channels: 2, PCM: []byte{0, 0, 0, 0}}
	err := encodeMP3("/nonexistent/dir/out.mp3", stats)
	if err == nil {
		t.Fatal("expected error for invalid output path")
	}
}
