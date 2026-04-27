// gen_ong_pcm: extracts two events from the WAV, normalises them, writes
// verification WAVs and a Go source file with the embedded PCM arrays.
package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
)

func main() {
	path := "recording-4-26-2026,-8-45-41-PM.wav"
	sr, pcm, err := readWAV(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read: %v\n", err)
		os.Exit(1)
	}
	mono := toMono(pcm)

	events := detectEvents(mono, sr)
	fmt.Fprintf(os.Stderr, "Events found: %d\n", len(events))
	for i, e := range events {
		fmt.Fprintf(os.Stderr, "  [%d] %.3fs  %.1fms  peak=%.4f\n",
			i+1,
			float64(e.start)/float64(sr),
			float64(e.end-e.start)/float64(sr)*1000,
			peakAmp(mono[e.start:e.end]))
	}

	if len(events) < 8 {
		fmt.Fprintln(os.Stderr, "need at least 8 events")
		os.Exit(1)
	}

	// Extract events 4 and 8 (1-indexed), 5ms pad each side.
	ev4pcm := extractNormalized(mono, events[3], sr, 0.45)
	ev8pcm := extractNormalized(mono, events[7], sr, 0.45)

	// Write verification WAVs
	writeVerificationWAV("out_ong_part1.wav", sr, ev4pcm)
	writeVerificationWAV("out_ong_part2.wav", sr, ev8pcm)

	// Two versions with a 300ms gap:
	//   version_a: event4 → gap → event8
	//   version_b: event8 → gap → event4
	const gapMs = 300
	gapFrames := gapMs * sr / 1000
	gapPCM := make([]byte, gapFrames*4)

	versionA := concat(ev4pcm, gapPCM, ev8pcm)
	versionB := concat(ev8pcm, gapPCM, ev4pcm)

	writeVerificationWAV("out_ong_version_a.wav", sr, versionA) // 4 → 8
	writeVerificationWAV("out_ong_version_b.wav", sr, versionB) // 8 → 4

	fmt.Fprintf(os.Stderr, "\nPart 1 (ev4): %.1fms\n", float64(len(ev4pcm)/4)/float64(sr)*1000)
	fmt.Fprintf(os.Stderr, "Part 2 (ev8): %.1fms\n", float64(len(ev8pcm)/4)/float64(sr)*1000)
	fmt.Fprintf(os.Stderr, "Gap:          %dms\n", gapMs)
	fmt.Fprintf(os.Stderr, "version_a (4→8): %.1fms\n", float64(len(versionA)/4)/float64(sr)*1000)
	fmt.Fprintf(os.Stderr, "version_b (8→4): %.1fms\n", float64(len(versionB)/4)/float64(sr)*1000)
	fmt.Fprintf(os.Stderr, "\nWrote: out_ong_version_a.wav (4→8), out_ong_version_b.wav (8→4)\n")
}

func concat(parts ...[]byte) []byte {
	total := 0
	for _, p := range parts {
		total += len(p)
	}
	out := make([]byte, 0, total)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

type eventSpan struct{ start, end int }

func detectEvents(mono []float64, sr int) []eventSpan {
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

	var events []eventSpan
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
			events = append(events, eventSpan{evStart, end})
			lastEnd = end
		}
	}
	if inEvent {
		events = append(events, eventSpan{evStart, len(mono)})
	}
	return events
}

// extractNormalized grabs an event with 5ms padding and normalises to targetPeak.
func extractNormalized(mono []float64, ev eventSpan, sr int, targetPeak float64) []byte {
	pad := sr * 5 / 1000
	start := ev.start - pad
	if start < 0 {
		start = 0
	}
	end := ev.end + pad
	if end > len(mono) {
		end = len(mono)
	}
	sig := make([]float64, end-start)
	copy(sig, mono[start:end])

	peak := peakAmp(sig)
	if peak > 0 {
		gain := targetPeak / peak
		for i := range sig {
			sig[i] *= gain
		}
	}

	out := make([]byte, len(sig)*4)
	for i, v := range sig {
		if v > 1.0 {
			v = 1.0
		} else if v < -1.0 {
			v = -1.0
		}
		s := int16(v * 32767)
		binary.LittleEndian.PutUint16(out[i*4:], uint16(s))
		binary.LittleEndian.PutUint16(out[i*4+2:], uint16(s))
	}
	return out
}

func writeGoByteSlice(f *os.File, name, comment string, data []byte) {
	fmt.Fprintf(f, "// %s is %s.\n", name, comment)
	fmt.Fprintf(f, "var %s = []byte{\n", name)
	for i, b := range data {
		if i%16 == 0 {
			fmt.Fprintf(f, "\t")
		}
		fmt.Fprintf(f, "0x%02x,", b)
		if i%16 == 15 {
			fmt.Fprintf(f, "\n")
		} else {
			fmt.Fprintf(f, " ")
		}
	}
	if len(data)%16 != 0 {
		fmt.Fprintf(f, "\n")
	}
	fmt.Fprintf(f, "}\n")
}

func writeVerificationWAV(path string, sr int, pcm []byte) {
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create %s: %v\n", path, err)
		return
	}
	defer f.Close()
	dataSize := uint32(len(pcm))
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
	f.Write(pcm)
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
	for {
		var id [4]byte
		if err := binary.Read(f, binary.LittleEndian, &id); err != nil {
			break
		}
		var size uint32
		binary.Read(f, binary.LittleEndian, &size)
		switch string(id[:]) {
		case "fmt ":
			var audioFmt, channels uint16
			binary.Read(f, binary.LittleEndian, &audioFmt)
			binary.Read(f, binary.LittleEndian, &channels)
			var srate uint32
			binary.Read(f, binary.LittleEndian, &srate)
			sampleRate = int(srate)
			rest := make([]byte, size-8)
			io.ReadFull(f, rest)
		case "data":
			data := make([]byte, size)
			io.ReadFull(f, data)
			pcm = make([]int16, len(data)/2)
			for i := range pcm {
				pcm[i] = int16(binary.LittleEndian.Uint16(data[i*2 : i*2+2]))
			}
			return sampleRate, pcm, nil
		default:
			io.ReadFull(f, make([]byte, size))
		}
	}
	return sampleRate, pcm, nil
}

func toMono(pcm []int16) []float64 {
	frames := len(pcm) / 2
	mono := make([]float64, frames)
	for i := 0; i < frames; i++ {
		mono[i] = (float64(pcm[i*2]) + float64(pcm[i*2+1])) / (2 * 32768.0)
	}
	return mono
}

func peakAmp(sig []float64) float64 {
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
