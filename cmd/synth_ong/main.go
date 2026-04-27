// synth_ong: synthesises a PS1-style click from measured spectral data
// and writes it to out_synthesized.wav for perceptual verification.
package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
)

const sampleRate = 44100

func main() {
	// ── Parameters derived from WAV analysis ─────────────────────────────────
	//
	// Dominant frequencies measured from the recording (first 20ms of best event):
	//   1765 Hz, 1679 Hz, 2497 Hz, 2368 Hz, 7665 Hz, 7838 Hz
	//
	// Envelope: slow-rise (~10ms), relatively sustained over ~150ms, then decay.
	// Character: mechanical click — sharp noise burst at onset + resonant body.

	const totalMs = 170 // ≤ 200ms
	numFrames := totalMs * sampleRate / 1000

	// Amplitude envelope: fast attack (5ms), sustain plateau, then exponential decay.
	const attackMs = 5
	const sustainMs = 30
	attackFrames := attackMs * sampleRate / 1000
	sustainFrames := sustainMs * sampleRate / 1000
	decayFrames := numFrames - attackFrames - sustainFrames

	mono := make([]float64, numFrames)

	for i := 0; i < numFrames; i++ {
		t := float64(i) / sampleRate

		// Amplitude envelope
		var env float64
		switch {
		case i < attackFrames:
			env = float64(i) / float64(attackFrames)
		case i < attackFrames+sustainFrames:
			env = 1.0
		default:
			decayPos := i - attackFrames - sustainFrames
			env = math.Exp(-5.0 * float64(decayPos) / float64(decayFrames))
		}

		// Resonant body: two pairs of close partials (measured)
		body := 0.0
		body += 0.40 * math.Sin(2*math.Pi*1765*t)
		body += 0.30 * math.Sin(2*math.Pi*1679*t)
		body += 0.20 * math.Sin(2*math.Pi*2497*t)
		body += 0.15 * math.Sin(2*math.Pi*2368*t)

		// High-frequency shimmer (measured ~7.7kHz region)
		shimmer := 0.0
		shimmer += 0.10 * math.Sin(2*math.Pi*7665*t)
		shimmer += 0.08 * math.Sin(2*math.Pi*7838*t)
		// Shimmer decays faster
		shimmerEnv := math.Exp(-8.0 * float64(i) / float64(numFrames))
		shimmer *= shimmerEnv

		// Noise click at onset (broadband transient for perceptual attack)
		click := 0.0
		const clickMs = 8
		clickFrames := clickMs * sampleRate / 1000
		if i < clickFrames {
			clickEnv := math.Exp(-6.0 * float64(i) / float64(clickFrames))
			// Inharmonic noise via multiplied sinusoids at prime-ish ratios
			click = clickEnv * (
				math.Sin(float64(i)*947.3) *
				math.Cos(float64(i)*563.1) *
				math.Sin(float64(i)*1237.7))
		}

		sample := env*(body+shimmer) + 0.35*click
		mono[i] = sample
	}

	// Normalise to ~-6 dBFS (0.5)
	peak := 0.0
	for _, v := range mono {
		if a := math.Abs(v); a > peak {
			peak = a
		}
	}
	if peak > 0 {
		gain := 0.5 / peak
		for i := range mono {
			mono[i] *= gain
		}
	}

	// Convert to stereo 16-bit PCM
	pcm := make([]int16, len(mono)*2)
	for i, v := range mono {
		s := int16(v * 32767)
		pcm[i*2] = s
		pcm[i*2+1] = s
	}

	if err := writeWAV("out_synthesized.wav", sampleRate, pcm); err != nil {
		fmt.Fprintf(os.Stderr, "write: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Wrote → out_synthesized.wav")
	fmt.Printf("Duration: %dms, %d frames\n", totalMs, numFrames)
}

func writeWAV(path string, sr int, pcm []int16) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	dataSize := uint32(len(pcm) * 2)
	binary.Write(f, binary.LittleEndian, []byte("RIFF"))
	binary.Write(f, binary.LittleEndian, uint32(36+dataSize))
	binary.Write(f, binary.LittleEndian, []byte("WAVE"))
	binary.Write(f, binary.LittleEndian, []byte("fmt "))
	binary.Write(f, binary.LittleEndian, uint32(16))
	binary.Write(f, binary.LittleEndian, uint16(1))        // PCM
	binary.Write(f, binary.LittleEndian, uint16(2))        // stereo
	binary.Write(f, binary.LittleEndian, uint32(sr))
	binary.Write(f, binary.LittleEndian, uint32(sr*4))     // byte rate
	binary.Write(f, binary.LittleEndian, uint16(4))        // block align
	binary.Write(f, binary.LittleEndian, uint16(16))       // bits/sample
	binary.Write(f, binary.LittleEndian, []byte("data"))
	binary.Write(f, binary.LittleEndian, dataSize)
	for _, s := range pcm {
		binary.Write(f, binary.LittleEndian, s)
	}
	return nil
}
