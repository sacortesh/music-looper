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
	"github.com/madelynnblue/go-dsp/fft"
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
	verbose := fs.Bool("verbose", false, "print detailed progress information")

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

	if *verbose {
		fmt.Fprintf(os.Stderr, "[verbose] Decoding %s...\n", inputPath)
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

	if *verbose {
		fmt.Fprintf(os.Stderr, "[verbose] Converting to mono and downsampling to 11025 Hz...\n")
	}
	mono := pcmToMono(stats.PCM, stats.SampleRate, 11025)
	monoDur := time.Duration(float64(len(mono.Samples)) / float64(mono.SampleRate) * float64(time.Second))
	fmt.Printf("Mono signal:  %d samples @ %d Hz (%s)\n", len(mono.Samples), mono.SampleRate, monoDur.Round(time.Millisecond))

	maxLoopSec := *maxLoop
	if maxLoopSec <= 0 {
		maxLoopSec = 0 // detectLoop will default to half-track
	}

	if *verbose {
		fmt.Fprintf(os.Stderr, "[verbose] Detecting loop (FFT-based autocorrelation, %d samples)...\n", len(mono.Samples))
		lastPct := -1
		progressReporter = func(pct float64, msg string) {
			iPct := int(pct)
			if iPct != lastPct && iPct%10 == 0 {
				fmt.Fprintf(os.Stderr, "\r[progress] %s... %d%%", msg, iPct)
				lastPct = iPct
			}
		}
	} else {
		progressReporter = nil
	}
	t0 := time.Now()
	loop := detectLoop(mono, *minLoop, maxLoopSec)
	if *verbose {
		fmt.Fprintf(os.Stderr, "\r[verbose] Loop detection completed in %s\n", time.Since(t0).Round(time.Millisecond))
	}

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
	if *verbose {
		fmt.Fprintf(os.Stderr, "[verbose] Extending audio to %s with %dms crossfade...\n", targetDur.Round(time.Millisecond), *crossfade)
	}
	extendedPCM := extendAudio(stats.PCM, stats.SampleRate, loop, targetDur, *crossfade)
	if *verbose {
		fmt.Fprintf(os.Stderr, "\r[progress] Extending audio... 100%%\n")
	}
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
	if *verbose {
		fmt.Fprintf(os.Stderr, "[verbose] Encoding MP3 to %s...\n", outputPath)
	}
	if err := encodeMP3(outputPath, outStats); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	fmt.Printf("Output:       %s\n", outputPath)
	if *verbose {
		fmt.Fprintf(os.Stderr, "[verbose] Done.\n")
	}
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

// nextPow2 returns the smallest power of 2 >= n.
func nextPow2(n int) int {
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}

// detectLoop finds the longest repeating loop in the mono signal using
// FFT-based normalized autocorrelation. minLoopSec sets the minimum loop
// duration in seconds; maxLoopSec sets the maximum (0 = half track length).
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
		dur := time.Duration(float64(n) / float64(mono.SampleRate) * float64(time.Second))
		return &LoopResult{Start: 0, End: dur, Length: dur, Correlation: 0}
	}

	// FFT-based autocorrelation: R(τ) = IFFT(|FFT(x)|²)
	// Pad to next power of 2 (at least 2*n) to avoid circular correlation artifacts.
	fftSize := nextPow2(2 * n)
	padded := make([]float64, fftSize)
	copy(padded, mono.Samples)

	// Forward FFT
	X := fft.FFTReal(padded)

	// Power spectrum: X * conj(X) = |X|²
	for i := range X {
		X[i] = complex(real(X[i])*real(X[i])+imag(X[i])*imag(X[i]), 0)
	}

	// Inverse FFT to get unnormalized autocorrelation
	R := fft.IFFT(X)

	// Precompute cumulative sum of squares for normalization.
	// For lag τ, energyA = Σ x(t)² for t in [0, n-τ), energyB = Σ x(t+τ)² for t in [0, n-τ).
	// Using prefix sums: energyA(τ) = prefixSq[n-τ], energyB(τ) = totalSq - prefixSq[τ]
	// where prefixSq[k] = Σ x(i)² for i in [0,k) and totalSq - prefixSq[τ] accounts
	// for the tail portion that only needs the overlap length.
	prefixSq := make([]float64, n+1)
	for i := 0; i < n; i++ {
		prefixSq[i+1] = prefixSq[i] + mono.Samples[i]*mono.Samples[i]
	}

	bestLag := minLag
	bestCorr := -1.0

	totalLags := maxLag - minLag + 1
	progressStep := totalLags / 20
	if progressStep < 1 {
		progressStep = 1
	}

	for lag := minLag; lag <= maxLag; lag++ {
		if progressReporter != nil && (lag-minLag)%progressStep == 0 {
			pct := float64(lag-minLag) / float64(totalLags) * 100
			progressReporter(pct, "Scanning autocorrelation")
		}
		unnorm := real(R[lag])
		// energyA = sum of x[0..n-lag)² = prefixSq[n-lag]
		energyA := prefixSq[n-lag]
		// energyB = sum of x[lag..n)² = prefixSq[n] - prefixSq[lag]
		energyB := prefixSq[n] - prefixSq[lag]
		denom := math.Sqrt(energyA * energyB)
		if denom == 0 {
			continue
		}
		corr := unnorm / denom
		if corr > bestCorr {
			bestCorr = corr
			bestLag = lag
		}
	}

	loopSec := float64(bestLag) / float64(mono.SampleRate)
	loopDur := time.Duration(loopSec * float64(time.Second))
	totalDur := time.Duration(float64(n) / float64(mono.SampleRate) * float64(time.Second))

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

// progressReporter is called during long operations with (percent, message).
// Set to nil to disable progress reporting.
var progressReporter func(pct float64, msg string)

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
		if progressReporter != nil {
			pct := float64(len(out)/bytesPerFrame) / float64(targetFrames) * 100
			progressReporter(pct, "Extending audio")
		}
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
