// analyze_wav: reads a WAV, detects sound events, prints their timing/envelope,
// and writes one isolated event to out_event.wav for verification.
package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"math/cmplx"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: analyze_wav <file.wav>")
		os.Exit(1)
	}
	path := os.Args[1]

	sr, pcm, err := readWAV(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Sample rate:  %d Hz\n", sr)
	fmt.Printf("Frames:       %d  (%.3fs)\n", len(pcm), float64(len(pcm))/float64(sr))

	// Convert to mono float64 [-1,1]
	mono := toMono(pcm)

	// ── Event detection ──────────────────────────────────────────────────────
	// RMS over 5ms windows; threshold = 10× median RMS.
	hopSamples := sr * 5 / 1000
	if hopSamples < 1 {
		hopSamples = 1
	}
	numHops := len(mono) / hopSamples
	rms := make([]float64, numHops)
	for i := range rms {
		s, e := i*hopSamples, (i+1)*hopSamples
		if e > len(mono) {
			e = len(mono)
		}
		var sum float64
		for _, v := range mono[s:e] {
			sum += v * v
		}
		rms[i] = math.Sqrt(sum / float64(e-s))
	}

	sorted := make([]float64, len(rms))
	copy(sorted, rms)
	// simple median
	med := median(sorted)
	threshold := med * 10
	if threshold < 0.005 {
		threshold = 0.005
	}
	fmt.Printf("RMS median:   %.6f  threshold: %.6f\n\n", med, threshold)

	// Collect event onsets (leading edge of each burst).
	type event struct{ start, end int }
	var events []event
	inEvent := false
	var evStart int
	minGapSamples := sr / 5 // 200ms minimum gap between events
	lastEnd := -minGapSamples

	for i, v := range rms {
		sample := i * hopSamples
		if !inEvent && v > threshold && (sample-lastEnd) > minGapSamples {
			inEvent = true
			evStart = sample
		} else if inEvent && v < threshold*0.3 {
			inEvent = false
			end := i*hopSamples + hopSamples
			events = append(events, event{evStart, end})
			lastEnd = end
		}
	}
	if inEvent {
		events = append(events, event{evStart, len(mono)})
	}

	fmt.Printf("Events found: %d\n", len(events))
	for i, ev := range events {
		dur := float64(ev.end-ev.start) / float64(sr) * 1000
		peak := peakAmplitude(mono[ev.start:ev.end])
		fmt.Printf("  [%d] start=%.3fs  dur=%.1fms  peak=%.4f\n",
			i+1,
			float64(ev.start)/float64(sr),
			dur,
			peak,
		)
	}

	if len(events) == 0 {
		fmt.Fprintln(os.Stderr, "no events detected — adjust threshold")
		os.Exit(1)
	}

	// ── Analyze event with highest peak ──────────────────────────────────────
	best := 0
	for i, ev := range events {
		if peakAmplitude(mono[ev.start:ev.end]) > peakAmplitude(mono[events[best].start:events[best].end]) {
			best = i
		}
	}
	ev := events[best]
	sig := mono[ev.start:ev.end]
	fmt.Printf("\nAnalyzing event [%d] (highest peak, %d samples, %.1fms):\n", best+1,
		len(sig), float64(len(sig))/float64(sr)*1000)

	// Envelope shape (print 10 points)
	fmt.Println("Envelope (10 points, normalized):")
	peakVal := peakAmplitude(sig)
	for i := 0; i < 10; i++ {
		pos := i * len(sig) / 10
		end := pos + len(sig)/10
		if end > len(sig) {
			end = len(sig)
		}
		chunk := sig[pos:end]
		r := 0.0
		for _, v := range chunk {
			r += v * v
		}
		r = math.Sqrt(r / float64(len(chunk)))
		bar := int(r / peakVal * 40)
		fmt.Printf("  %3d%%: %.4f  %s\n", i*10, r, repeatChar('#', bar))
	}

	// Dominant frequencies via DFT on first 20ms
	analysisSamples := sr * 20 / 1000
	if analysisSamples > len(sig) {
		analysisSamples = len(sig)
	}
	freqs := dominantFreqs(sig[:analysisSamples], sr, 6)
	fmt.Println("\nDominant frequencies (first 20ms):")
	for _, f := range freqs {
		fmt.Printf("  %.1f Hz  (mag %.4f)\n", f.hz, f.mag)
	}

	// Attack time: samples to reach 90% of peak
	attackSamples := 0
	for i, v := range sig {
		if math.Abs(v) >= 0.9*peakVal {
			attackSamples = i
			break
		}
	}
	fmt.Printf("\nAttack time:  %.2fms (%d samples)\n",
		float64(attackSamples)/float64(sr)*1000, attackSamples)

	// Decay: where amplitude drops to 10% of peak (after attack)
	decaySamples := len(sig)
	for i := attackSamples; i < len(sig); i++ {
		if math.Abs(sig[i]) < 0.1*peakVal {
			decaySamples = i - attackSamples
			break
		}
	}
	fmt.Printf("Decay time:   %.2fms (%d samples)\n",
		float64(decaySamples)/float64(sr)*1000, decaySamples)

	// ── Write all events as individual WAV files ─────────────────────────────
	padSamples := sr * 10 / 1000
	fmt.Printf("\nWriting individual event WAVs:\n")
	for i, e := range events {
		ws := e.start - padSamples
		if ws < 0 {
			ws = 0
		}
		we := e.end + padSamples
		if we > len(pcm) {
			we = len(pcm)
		}
		outPath := fmt.Sprintf("out_event_%d.wav", i+1)
		if err := writeWAV(outPath, sr, pcm[ws:we]); err != nil {
			fmt.Fprintf(os.Stderr, "  write %s: %v\n", outPath, err)
			continue
		}
		marker := ""
		if i == best {
			marker = "  ← analyzed (highest peak)"
		}
		fmt.Printf("  %s  (%.1fms)%s\n", outPath,
			float64(we-ws)/float64(sr)*1000, marker)
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

func readWAV(path string) (sampleRate int, pcm []int16, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, nil, err
	}
	defer f.Close()

	// RIFF header
	var riff [4]byte
	binary.Read(f, binary.LittleEndian, &riff)
	if string(riff[:]) != "RIFF" {
		return 0, nil, fmt.Errorf("not a RIFF file")
	}
	var chunkSize uint32
	binary.Read(f, binary.LittleEndian, &chunkSize)
	var wave [4]byte
	binary.Read(f, binary.LittleEndian, &wave)
	if string(wave[:]) != "WAVE" {
		return 0, nil, fmt.Errorf("not a WAVE file")
	}

	var sr int
	// scan chunks
	for {
		var id [4]byte
		if err := binary.Read(f, binary.LittleEndian, &id); err != nil {
			break
		}
		var size uint32
		binary.Read(f, binary.LittleEndian, &size)

		switch string(id[:]) {
		case "fmt ":
			var audioFmt uint16
			binary.Read(f, binary.LittleEndian, &audioFmt)
			var channels uint16
			binary.Read(f, binary.LittleEndian, &channels)
			var srate uint32
			binary.Read(f, binary.LittleEndian, &srate)
			sr = int(srate)
			rest := make([]byte, size-8)
			io.ReadFull(f, rest)
		case "data":
			data := make([]byte, size)
			io.ReadFull(f, data)
			pcm = make([]int16, len(data)/2)
			for i := range pcm {
				pcm[i] = int16(binary.LittleEndian.Uint16(data[i*2 : i*2+2]))
			}
			return sr, pcm, nil
		default:
			io.ReadFull(f, make([]byte, size))
		}
	}
	return sr, pcm, nil
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
	binary.Write(f, binary.LittleEndian, uint16(1))  // PCM
	binary.Write(f, binary.LittleEndian, uint16(2))  // stereo
	binary.Write(f, binary.LittleEndian, uint32(sr))
	binary.Write(f, binary.LittleEndian, uint32(sr*4)) // byte rate
	binary.Write(f, binary.LittleEndian, uint16(4))    // block align
	binary.Write(f, binary.LittleEndian, uint16(16))   // bits per sample
	binary.Write(f, binary.LittleEndian, []byte("data"))
	binary.Write(f, binary.LittleEndian, dataSize)
	for _, s := range pcm {
		binary.Write(f, binary.LittleEndian, s)
	}
	return nil
}

func toMono(pcm []int16) []float64 {
	// stereo interleaved: L R L R ...
	frames := len(pcm) / 2
	mono := make([]float64, frames)
	for i := 0; i < frames; i++ {
		l := float64(pcm[i*2])
		r := float64(pcm[i*2+1])
		mono[i] = (l + r) / (2 * 32768.0)
	}
	return mono
}

func peakAmplitude(sig []float64) float64 {
	p := 0.0
	for _, v := range sig {
		if a := math.Abs(v); a > p {
			p = a
		}
	}
	return p
}

func median(s []float64) float64 {
	// insertion sort (small enough)
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
	return s[len(s)/2]
}

type freqMag struct {
	hz, mag float64
}

func dominantFreqs(sig []float64, sr, n int) []freqMag {
	// zero-pad to next power of 2
	size := 1
	for size < len(sig) {
		size <<= 1
	}
	x := make([]complex128, size)
	for i, v := range sig {
		x[i] = complex(v, 0)
	}
	dft(x)

	// collect magnitudes for positive freqs
	type fm struct{ i int; m float64 }
	half := size / 2
	mags := make([]fm, half)
	for i := range mags {
		mags[i] = fm{i, cmplx.Abs(x[i])}
	}
	// sort descending
	for i := 1; i < len(mags); i++ {
		for j := i; j > 0 && mags[j].m > mags[j-1].m; j-- {
			mags[j], mags[j-1] = mags[j-1], mags[j]
		}
	}
	// pick top-n, spaced at least 50 Hz apart
	result := make([]freqMag, 0, n)
	for _, m := range mags {
		if len(result) >= n {
			break
		}
		hz := float64(m.i) * float64(sr) / float64(size)
		tooClose := false
		for _, r := range result {
			if math.Abs(hz-r.hz) < 50 {
				tooClose = true
				break
			}
		}
		if !tooClose {
			result = append(result, freqMag{hz, m.m})
		}
	}
	return result
}

// Cooley-Tukey in-place DFT (power-of-2 size).
func dft(x []complex128) {
	n := len(x)
	if n <= 1 {
		return
	}
	// bit-reversal permutation
	j := 0
	for i := 1; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			x[i], x[j] = x[j], x[i]
		}
	}
	for length := 2; length <= n; length <<= 1 {
		ang := -2 * math.Pi / float64(length)
		wlen := complex(math.Cos(ang), math.Sin(ang))
		for i := 0; i < n; i += length {
			w := complex(1, 0)
			for k := 0; k < length/2; k++ {
				u, v := x[i+k], x[i+k+length/2]*w
				x[i+k] = u + v
				x[i+k+length/2] = u - v
				w *= wlen
			}
		}
	}
}

func repeatChar(c byte, n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = c
	}
	return string(b)
}
