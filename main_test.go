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
