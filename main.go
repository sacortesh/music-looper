package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"time"

	"github.com/hajimehoshi/go-mp3"
	"github.com/viert/go-lame"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: %s <input.mp3> <target-minutes>\n", os.Args[0])
		os.Exit(1)
	}

	inputPath := os.Args[1]
	targetMinutes, err := strconv.ParseFloat(os.Args[2], 64)
	if err != nil || targetMinutes <= 0 {
		fmt.Fprintf(os.Stderr, "Error: target-minutes must be a positive number\n")
		os.Exit(1)
	}

	stats, err := decodeMP3(inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Input:        %s\n", inputPath)
	fmt.Printf("Sample rate:  %d Hz\n", stats.SampleRate)
	fmt.Printf("Channels:     %d\n", stats.Channels)
	fmt.Printf("Duration:     %s\n", stats.Duration.Round(time.Millisecond))
	fmt.Printf("Samples:      %d\n", stats.SampleCount)
	fmt.Printf("Target:       %.1f minutes\n", targetMinutes)

	mono := pcmToMono(stats.PCM, stats.SampleRate, 11025)
	monoDur := time.Duration(float64(len(mono.Samples)) / float64(mono.SampleRate) * float64(time.Second))
	fmt.Printf("Mono signal:  %d samples @ %d Hz (%s)\n", len(mono.Samples), mono.SampleRate, monoDur.Round(time.Millisecond))

	outputPath := inputPath[:len(inputPath)-len(".mp3")] + "_loop.mp3"
	if err := encodeMP3(outputPath, stats); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Output:       %s\n", outputPath)
}

// MonoSignal holds a downsampled mono signal ready for analysis.
type MonoSignal struct {
	Samples    []float64
	SampleRate int
}

// pcmToMono converts stereo 16-bit PCM to a mono float64 signal,
// normalized to [-1.0, 1.0], and downsampled to targetRate Hz.
func pcmToMono(pcm []byte, srcRate, targetRate int) *MonoSignal {
	// Each stereo sample frame = 4 bytes (2 bytes L + 2 bytes R)
	frameCount := len(pcm) / 4

	// Step 1: Convert to mono float64 normalized to [-1.0, 1.0]
	mono := make([]float64, frameCount)
	for i := 0; i < frameCount; i++ {
		off := i * 4
		left := int16(binary.LittleEndian.Uint16(pcm[off : off+2]))
		right := int16(binary.LittleEndian.Uint16(pcm[off+2 : off+4]))
		mono[i] = (float64(left) + float64(right)) / (2.0 * 32768.0)
	}

	// Step 2: Downsample via simple decimation with averaging
	ratio := float64(srcRate) / float64(targetRate)
	outLen := int(math.Floor(float64(frameCount) / ratio))
	downsampled := make([]float64, outLen)
	for i := 0; i < outLen; i++ {
		center := float64(i) * ratio
		idx := int(center)
		if idx >= frameCount {
			idx = frameCount - 1
		}
		downsampled[i] = mono[idx]
	}

	return &MonoSignal{
		Samples:    downsampled,
		SampleRate: targetRate,
	}
}

// AudioStats holds metadata about a decoded MP3.
type AudioStats struct {
	SampleRate  int
	Channels    int
	Duration    time.Duration
	SampleCount int64
	PCM         []byte
}

// decodeMP3 reads an MP3 file and returns its PCM data and stats.
func decodeMP3(path string) (*AudioStats, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	decoder, err := mp3.NewDecoder(f)
	if err != nil {
		return nil, fmt.Errorf("decode mp3: %w", err)
	}

	pcm, err := io.ReadAll(decoder)
	if err != nil {
		return nil, fmt.Errorf("read pcm: %w", err)
	}

	sampleRate := decoder.SampleRate()
	channels := 2 // go-mp3 always decodes to stereo 16-bit
	bytesPerSample := 2 * channels
	sampleCount := int64(len(pcm)) / int64(bytesPerSample)
	duration := time.Duration(float64(sampleCount) / float64(sampleRate) * float64(time.Second))

	return &AudioStats{
		SampleRate:  sampleRate,
		Channels:    channels,
		Duration:    duration,
		SampleCount: sampleCount,
		PCM:         pcm,
	}, nil
}

// encodeMP3 writes PCM data from AudioStats to an MP3 file.
func encodeMP3(path string, stats *AudioStats) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()

	enc := lame.NewEncoder(f)
	defer enc.Close()

	enc.SetInSamplerate(stats.SampleRate)
	enc.SetNumChannels(stats.Channels)
	if err := enc.SetQuality(2); err != nil {
		return fmt.Errorf("set quality: %w", err)
	}

	if _, err := enc.Write(stats.PCM); err != nil {
		return fmt.Errorf("encode pcm: %w", err)
	}

	return nil
}
