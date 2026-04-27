// extract_event: extracts one event from the WAV, normalises it, writes a
// verification WAV, and dumps the Go byte-slice literal to stdout.
package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: extract_event <file.wav> <event-number>")
		os.Exit(1)
	}
	path := os.Args[1]
	evNum, err := strconv.Atoi(os.Args[2])
	if err != nil || evNum < 1 {
		fmt.Fprintln(os.Stderr, "event-number must be a positive integer")
		os.Exit(1)
	}

	sr, pcm, err := readWAV(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read: %v\n", err)
		os.Exit(1)
	}

	mono := toMono(pcm)

	// ── Event detection (same as analyze_wav) ────────────────────────────────
	hopSamples := sr * 5 / 1000
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
	med := median(sorted)
	threshold := med * 10
	if threshold < 0.005 {
		threshold = 0.005
	}

	type event struct{ start, end int }
	var events []event
	inEvent := false
	var evStart int
	minGap := sr / 5
	lastEnd := -minGap
	for i, v := range rms {
		sample := i * hopSamples
		if !inEvent && v > threshold && (sample-lastEnd) > minGap {
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

	fmt.Fprintf(os.Stderr, "Events found: %d\n", len(events))
	for i, e := range events {
		dur := float64(e.end-e.start) / float64(sr) * 1000
		peak := peakAmplitude(mono[e.start:e.end])
		fmt.Fprintf(os.Stderr, "  [%d] %.3fs  %.1fms  peak=%.4f\n",
			i+1, float64(e.start)/float64(sr), dur, peak)
	}

	if evNum > len(events) {
		fmt.Fprintf(os.Stderr, "event %d does not exist (only %d found)\n", evNum, len(events))
		os.Exit(1)
	}

	ev := events[evNum-1]

	// Trim 5ms of padding from each side
	padSamples := sr * 5 / 1000
	start := ev.start - padSamples
	if start < 0 {
		start = 0
	}
	end := ev.end + padSamples
	if end > len(mono) {
		end = len(mono)
	}
	sig := mono[start:end]

	durMs := float64(len(sig)) / float64(sr) * 1000
	peak := peakAmplitude(sig)
	fmt.Fprintf(os.Stderr, "\nExtracted event %d: %.1fms, peak=%.4f\n", evNum, durMs, peak)

	// Normalize to -6 dBFS (0.5 linear)
	targetPeak := 0.5
	if peak > 0 {
		gain := targetPeak / peak
		for i := range sig {
			sig[i] *= gain
		}
	}

	// Convert to stereo 16-bit PCM
	stereo := make([]int16, len(sig)*2)
	for i, v := range sig {
		s := int16(clamp(v, -1, 1) * 32767)
		stereo[i*2] = s
		stereo[i*2+1] = s
	}

	// Write verification WAV
	outWAV := fmt.Sprintf("out_event_%d_normalized.wav", evNum)
	if err := writeWAV(outWAV, sr, stereo); err != nil {
		fmt.Fprintf(os.Stderr, "write wav: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "Wrote → %s  (listen to verify)\n\n", outWAV)

	// Dump Go byte-slice literal to stdout
	rawBytes := make([]byte, len(stereo)*2)
	for i, s := range stereo {
		binary.LittleEndian.PutUint16(rawBytes[i*2:], uint16(s))
	}

	fmt.Printf("// ongTransientPCM is stereo 16-bit little-endian PCM at 44100 Hz, %.0fms.\n", durMs)
	fmt.Printf("// Generated from a real PS1 disc-reload recording.\n")
	fmt.Printf("var ongTransientPCM = []byte{\n")
	for i, b := range rawBytes {
		if i%16 == 0 {
			fmt.Printf("\t")
		}
		fmt.Printf("0x%02x,", b)
		if i%16 == 15 {
			fmt.Printf("\n")
		} else {
			fmt.Printf(" ")
		}
	}
	if len(rawBytes)%16 != 0 {
		fmt.Printf("\n")
	}
	fmt.Printf("}\n")
	fmt.Printf("\nconst ongTransientSampleRate = %d\n", sr)
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func readWAV(path string) (sampleRate int, pcm []int16, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, nil, err
	}
	defer f.Close()
	var riff [4]byte
	binary.Read(f, binary.LittleEndian, &riff)
	if string(riff[:]) != "RIFF" {
		return 0, nil, fmt.Errorf("not a RIFF file")
	}
	var chunkSize uint32
	binary.Read(f, binary.LittleEndian, &chunkSize)
	var wave [4]byte
	binary.Read(f, binary.LittleEndian, &wave)
	var sr int
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
	binary.Write(f, binary.LittleEndian, uint16(1))
	binary.Write(f, binary.LittleEndian, uint16(2))
	binary.Write(f, binary.LittleEndian, uint32(sr))
	binary.Write(f, binary.LittleEndian, uint32(sr*4))
	binary.Write(f, binary.LittleEndian, uint16(4))
	binary.Write(f, binary.LittleEndian, uint16(16))
	binary.Write(f, binary.LittleEndian, []byte("data"))
	binary.Write(f, binary.LittleEndian, dataSize)
	for _, s := range pcm {
		binary.Write(f, binary.LittleEndian, s)
	}
	return nil
}

func toMono(pcm []int16) []float64 {
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
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
	return s[len(s)/2]
}
