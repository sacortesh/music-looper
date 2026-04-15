package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hajimehoshi/go-mp3"
	"github.com/viert/go-lame"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// run contains the main logic, returning an exit code. This makes testing easier.
func run(args []string) int {
	fs := flag.NewFlagSet("music-loop", flag.ContinueOnError)
	output := fs.String("output", "", "output file path (default: <input>_loop.mp3)")
	minLoop := fs.Float64("min-loop", 10.0, "minimum loop duration in seconds")
	maxLoop := fs.Float64("max-loop", 0, "maximum loop duration in seconds (default: half track)")
	crossfade := fs.Int("crossfade", 50, "crossfade duration in milliseconds")
	dryRun := fs.Bool("dry-run", false, "analyze only, do not write output file")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: music-loop [flags] <input.mp3> <target-minutes>\n\nFlags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return 1
	}

	if fs.NArg() < 2 {
		fs.Usage()
		return 1
	}

	inputPath := fs.Arg(0)
	targetMinutes, err := parseTargetMinutes(fs.Arg(1))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	if err := validateInput(inputPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	if *minLoop < 0 {
		fmt.Fprintf(os.Stderr, "Error: --min-loop must be non-negative\n")
		return 1
	}
	if *maxLoop < 0 {
		fmt.Fprintf(os.Stderr, "Error: --max-loop must be non-negative\n")
		return 1
	}
	if *crossfade < 0 {
		fmt.Fprintf(os.Stderr, "Error: --crossfade must be non-negative\n")
		return 1
	}

	stats, err := decodeMP3(inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
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

	maxLoopSec := *maxLoop
	if maxLoopSec <= 0 {
		maxLoopSec = 0 // detectLoop will default to half-track
	}

	loop := detectLoop(mono, *minLoop, maxLoopSec)

	if loop.Correlation < 0.5 {
		fmt.Printf("Warning:      low correlation (%.4f), falling back to full-track loop\n", loop.Correlation)
		dur := time.Duration(float64(len(mono.Samples)) / float64(mono.SampleRate) * float64(time.Second))
		loop = &LoopResult{Start: 0, End: dur, Length: dur, Correlation: 0}
	}

	fmt.Printf("Loop start:   %s\n", loop.Start.Round(time.Millisecond))
	fmt.Printf("Loop end:     %s\n", loop.End.Round(time.Millisecond))
	fmt.Printf("Loop length:  %s\n", loop.Length.Round(time.Millisecond))
	fmt.Printf("Correlation:  %.4f\n", loop.Correlation)

	if *dryRun {
		fmt.Println("Dry run — skipping output.")
		return 0
	}

	targetDur := time.Duration(targetMinutes * float64(time.Minute))
	extendedPCM := extendAudio(stats.PCM, stats.SampleRate, loop, targetDur, *crossfade)
	extendedDur := time.Duration(float64(len(extendedPCM)/4) / float64(stats.SampleRate) * float64(time.Second))
	fmt.Printf("Extended:     %s\n", extendedDur.Round(time.Millisecond))

	outputPath := *output
	if outputPath == "" {
		outputPath = defaultOutputPath(inputPath)
	}
	outStats := &AudioStats{
		SampleRate:  stats.SampleRate,
		Channels:    stats.Channels,
		Duration:    extendedDur,
		SampleCount: int64(len(extendedPCM) / 4),
		PCM:         extendedPCM,
	}
	if err := encodeMP3(outputPath, outStats); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	fmt.Printf("Output:       %s\n", outputPath)
	return 0
}

// parseTargetMinutes parses and validates the target duration string.
func parseTargetMinutes(s string) (float64, error) {
	v, err := fmt.Sscanf(s, "%f", new(float64))
	if err != nil || v == 0 {
		return 0, fmt.Errorf("target-minutes must be a positive number, got %q", s)
	}
	var f float64
	fmt.Sscanf(s, "%f", &f)
	if f <= 0 {
		return 0, fmt.Errorf("target-minutes must be a positive number, got %q", s)
	}
	return f, nil
}

// validateInput checks that the input file exists and has an .mp3 extension.
func validateInput(path string) error {
	if !strings.EqualFold(filepath.Ext(path), ".mp3") {
		return fmt.Errorf("input file must have .mp3 extension: %s", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("cannot access input file: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("input path is a directory, not a file: %s", path)
	}
	return nil
}

// defaultOutputPath generates the output path by appending _loop before the extension.
func defaultOutputPath(inputPath string) string {
	ext := filepath.Ext(inputPath)
	base := strings.TrimSuffix(inputPath, ext)
	return base + "_loop" + ext
}

// LoopResult holds the detected loop boundaries and quality.
type LoopResult struct {
	Start       time.Duration
	End         time.Duration
	Length      time.Duration
	Correlation float64
}

// detectLoop finds the longest repeating loop in the mono signal using
// normalized autocorrelation. minLoopSec sets the minimum loop duration
// in seconds; maxLoopSec sets the maximum (0 = half track length).
func detectLoop(mono *MonoSignal, minLoopSec, maxLoopSec float64) *LoopResult {
	n := len(mono.Samples)
	minLag := int(minLoopSec * float64(mono.SampleRate))
	maxLag := n / 2
	if maxLoopSec > 0 {
		userMax := int(maxLoopSec * float64(mono.SampleRate))
		if userMax < maxLag {
			maxLag = userMax
		}
	}

	if minLag >= maxLag {
		// Track too short for loop detection — treat whole track as loop
		dur := time.Duration(float64(n) / float64(mono.SampleRate) * float64(time.Second))
		return &LoopResult{Start: 0, End: dur, Length: dur, Correlation: 0}
	}

	// Precompute the energy of the full overlap region for normalization.
	// For lag τ, we compare samples [0..n-τ) with [τ..n).
	// Normalized autocorrelation: r(τ) = Σ x(t)*x(t+τ) / sqrt(Σ x(t)² * Σ x(t+τ)²)

	bestLag := minLag
	bestCorr := -1.0

	for lag := minLag; lag <= maxLag; lag++ {
		overlapLen := n - lag
		var sum, energyA, energyB float64
		for t := 0; t < overlapLen; t++ {
			a := mono.Samples[t]
			b := mono.Samples[t+lag]
			sum += a * b
			energyA += a * a
			energyB += b * b
		}
		denom := math.Sqrt(energyA * energyB)
		if denom == 0 {
			continue
		}
		corr := sum / denom
		if corr > bestCorr {
			bestCorr = corr
			bestLag = lag
		}
	}

	loopSec := float64(bestLag) / float64(mono.SampleRate)
	loopDur := time.Duration(loopSec * float64(time.Second))
	totalDur := time.Duration(float64(n) / float64(mono.SampleRate) * float64(time.Second))

	// Loop starts at 0, ends at loop duration (the point where it repeats)
	endDur := loopDur
	if endDur > totalDur {
		endDur = totalDur
	}

	return &LoopResult{
		Start:       0,
		End:         endDur,
		Length:      loopDur,
		Correlation: bestCorr,
	}
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

// extendAudio repeats the detected loop to fill targetDur, applying a
// crossfade of crossfadeMs milliseconds at each loop junction.
func extendAudio(pcm []byte, sampleRate int, loop *LoopResult, targetDur time.Duration, crossfadeMs int) []byte {
	bytesPerFrame := 4 // stereo 16-bit
	totalFrames := len(pcm) / bytesPerFrame
	targetFrames := int(targetDur.Seconds() * float64(sampleRate))

	loopStartFrame := int(loop.Start.Seconds() * float64(sampleRate))
	loopEndFrame := int(loop.End.Seconds() * float64(sampleRate))
	if loopEndFrame > totalFrames {
		loopEndFrame = totalFrames
	}
	loopLen := loopEndFrame - loopStartFrame
	if loopLen <= 0 {
		// No valid loop — return original
		return pcm
	}

	crossfadeFrames := crossfadeMs * sampleRate / 1000
	if crossfadeFrames > loopLen/2 {
		crossfadeFrames = loopLen / 2
	}

	// Start with intro (everything before loop start)
	introBytes := loopStartFrame * bytesPerFrame
	out := make([]byte, 0, targetFrames*bytesPerFrame)
	out = append(out, pcm[:introBytes]...)

	// Append the first loop iteration fully
	loopBody := pcm[loopStartFrame*bytesPerFrame : loopEndFrame*bytesPerFrame]
	out = append(out, loopBody...)

	for len(out)/bytesPerFrame < targetFrames {
		// Trim the crossfade region from the tail — it will be re-created
		// as a blend of old tail + new head.
		cfLen := crossfadeFrames
		tailFrames := len(out) / bytesPerFrame
		if cfLen > tailFrames {
			cfLen = tailFrames
		}
		out = out[:len(out)-cfLen*bytesPerFrame]

		// Build the next iteration (may be truncated to hit target)
		needed := targetFrames - len(out)/bytesPerFrame
		iterLen := loopLen
		if iterLen > needed {
			iterLen = needed
		}
		if cfLen > iterLen {
			cfLen = iterLen
		}

		// Create crossfade blend for the first cfLen frames
		nextIter := make([]byte, iterLen*bytesPerFrame)
		copy(nextIter, loopBody[:iterLen*bytesPerFrame])

		// Read the removed tail for blending
		tailStart := len(out)
		// The removed tail bytes are still accessible (capacity preserved)
		removedTail := out[tailStart : tailStart+cfLen*bytesPerFrame]

		for i := 0; i < cfLen; i++ {
			alpha := float64(i) / float64(cfLen) // 0→1
			tOff := i * bytesPerFrame
			nOff := i * bytesPerFrame
			for ch := 0; ch < 2; ch++ {
				ti := tOff + ch*2
				ni := nOff + ch*2
				oldVal := int16(binary.LittleEndian.Uint16(removedTail[ti : ti+2]))
				newVal := int16(binary.LittleEndian.Uint16(nextIter[ni : ni+2]))
				blended := int16(float64(oldVal)*(1-alpha) + float64(newVal)*alpha)
				binary.LittleEndian.PutUint16(nextIter[ni:ni+2], uint16(blended))
			}
		}

		out = append(out, nextIter...)
	}

	return out
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
