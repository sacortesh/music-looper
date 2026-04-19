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

// loopOptions holds all processing parameters shared between single and batch modes.
type loopOptions struct {
	minLoop   float64
	maxLoop   float64
	crossfade int
	fadeOut   int
	dryRun    bool
	verbose   bool
}

// run contains the main logic, returning an exit code. This makes testing easier.
func run(args []string) int {
	fs := flag.NewFlagSet("music-loop", flag.ContinueOnError)
	output := fs.String("output", "", "output file or directory (default: <input>_loop.mp3, or <dir>/ for batch)")
	minLoop := fs.Float64("min-loop", 10.0, "minimum loop duration in seconds")
	maxLoop := fs.Float64("max-loop", 0, "maximum loop duration in seconds (default: half track)")
	crossfade := fs.Int("crossfade", 50, "crossfade duration in milliseconds")
	fadeOut := fs.Int("fade-out", 2000, "fade-out duration in milliseconds at the end of the output (0 to disable)")
	dryRun := fs.Bool("dry-run", false, "analyze only, do not write output file")
	verbose := fs.Bool("verbose", false, "print detailed progress information")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: music-loop [flags] <input.mp3|dir> <target-minutes>\n\nFlags:\n")
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

	opts := loopOptions{
		minLoop:   *minLoop,
		maxLoop:   *maxLoop,
		crossfade: *crossfade,
		fadeOut:   *fadeOut,
		dryRun:    *dryRun,
		verbose:   *verbose,
	}

	// Batch mode: input is a directory
	info, err := os.Stat(inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot access input: %v\n", err)
		return 1
	}
	if info.IsDir() {
		return runBatch(inputPath, *output, targetMinutes, opts)
	}

	// Single-file mode
	if err := validateInput(inputPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	outPath := *output
	if outPath == "" {
		outPath = defaultOutputPath(inputPath)
	}
	if err := processFile(inputPath, outPath, targetMinutes, opts); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}

// runBatch processes all .mp3 files in inputDir. If outputDir is non-empty,
// output files are written there; otherwise they are placed alongside each source file.
func runBatch(inputDir, outputDir string, targetMinutes float64, opts loopOptions) int {
	entries, err := os.ReadDir(inputDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot read directory %s: %v\n", inputDir, err)
		return 1
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".mp3") {
			files = append(files, filepath.Join(inputDir, e.Name()))
		}
	}

	if len(files) == 0 {
		fmt.Fprintf(os.Stderr, "Error: no .mp3 files found in %s\n", inputDir)
		return 1
	}

	if outputDir != "" {
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Error: cannot create output directory %s: %v\n", outputDir, err)
			return 1
		}
	}

	fmt.Printf("Batch:        %d file(s) in %s\n", len(files), inputDir)
	if outputDir != "" {
		fmt.Printf("Output dir:   %s\n", outputDir)
	}
	fmt.Println()

	failures := 0
	for i, f := range files {
		fmt.Printf("── [%d/%d] %s\n", i+1, len(files), filepath.Base(f))
		outPath := defaultOutputPath(f)
		if outputDir != "" {
			outPath = filepath.Join(outputDir, filepath.Base(defaultOutputPath(f)))
		}
		if err := processFile(f, outPath, targetMinutes, opts); err != nil {
			fmt.Fprintf(os.Stderr, "  Failed: %v\n", err)
			failures++
		}
		fmt.Println()
	}

	fmt.Printf("Batch done:   %d succeeded, %d failed\n", len(files)-failures, failures)
	if failures > 0 {
		return 1
	}
	return 0
}

// processFile runs the full loop-extension pipeline for a single MP3 file.
func processFile(inputPath, outputPath string, targetMinutes float64, opts loopOptions) error {
	if opts.verbose {
		fmt.Fprintf(os.Stderr, "[verbose] Decoding %s...\n", inputPath)
	}
	stats, err := decodeMP3(inputPath)
	if err != nil {
		return err
	}

	fmt.Printf("Input:        %s\n", inputPath)
	fmt.Printf("Sample rate:  %d Hz\n", stats.SampleRate)
	fmt.Printf("Channels:     %d\n", stats.Channels)
	fmt.Printf("Duration:     %s\n", stats.Duration.Round(time.Millisecond))
	fmt.Printf("Samples:      %d\n", stats.SampleCount)
	fmt.Printf("Target:       %.1f minutes\n", targetMinutes)

	if opts.verbose {
		fmt.Fprintf(os.Stderr, "[verbose] Converting to mono and downsampling to 11025 Hz...\n")
	}
	mono := pcmToMono(stats.PCM, stats.SampleRate, 11025)
	monoDur := time.Duration(float64(len(mono.Samples)) / float64(mono.SampleRate) * float64(time.Second))
	fmt.Printf("Mono signal:  %d samples @ %d Hz (%s)\n", len(mono.Samples), mono.SampleRate, monoDur.Round(time.Millisecond))

	maxLoopSec := opts.maxLoop

	if opts.verbose {
		fmt.Fprintf(os.Stderr, "[verbose] Detecting loop (energy-envelope loop-point search, %d samples)...\n", len(mono.Samples))
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
	loop := detectLoop(mono, opts.minLoop, maxLoopSec)
	if opts.verbose {
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

	if opts.dryRun {
		fmt.Println("Dry run — skipping output.")
		return nil
	}

	targetDur := time.Duration(targetMinutes * float64(time.Minute))
	if opts.verbose {
		fmt.Fprintf(os.Stderr, "[verbose] Extending audio to %s with %dms crossfade...\n", targetDur.Round(time.Millisecond), opts.crossfade)
	}
	extendedPCM := extendAudio(stats.PCM, stats.SampleRate, loop, targetDur, opts.crossfade, opts.fadeOut)
	if opts.verbose {
		fmt.Fprintf(os.Stderr, "\r[progress] Extending audio... 100%%\n")
	}
	extendedDur := time.Duration(float64(len(extendedPCM)/4) / float64(stats.SampleRate) * float64(time.Second))
	fmt.Printf("Extended:     %s\n", extendedDur.Round(time.Millisecond))

	outStats := &AudioStats{
		SampleRate:  stats.SampleRate,
		Channels:    stats.Channels,
		Duration:    extendedDur,
		SampleCount: int64(len(extendedPCM) / 4),
		PCM:         extendedPCM,
	}
	if opts.verbose {
		fmt.Fprintf(os.Stderr, "[verbose] Encoding MP3 to %s...\n", outputPath)
	}
	if err := encodeMP3(outputPath, outStats); err != nil {
		return err
	}
	fmt.Printf("Output:       %s\n", outputPath)
	if opts.verbose {
		fmt.Fprintf(os.Stderr, "[verbose] Done.\n")
	}
	return nil
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

// detectLoop finds the best loop start and end points in the mono signal.
//
// Strategy: compare energy envelopes between candidate loop-end positions (near the
// end of the song) and candidate loop-start positions (earlier in the song). The
// score balances similarity (Pearson correlation of 5-second energy windows) against
// loop length, strongly favouring longer loops. This produces the semantic the user
// expects: an intro that plays once followed by a long repeating body.
//
// minLoopSec sets the minimum loop duration; maxLoopSec sets the maximum (0 = full track).
func detectLoop(mono *MonoSignal, minLoopSec, maxLoopSec float64) *LoopResult {
	n := len(mono.Samples)
	sr := mono.SampleRate
	totalSec := float64(n) / float64(sr)
	totalDur := time.Duration(totalSec * float64(time.Second))

	// Energy envelope: RMS over 500ms hops.
	hopSec := 0.5
	hopSamples := int(hopSec * float64(sr))
	if hopSamples < 1 {
		hopSamples = 1
	}
	numHops := n / hopSamples
	if numHops < 4 {
		return &LoopResult{Start: 0, End: totalDur, Length: totalDur, Correlation: 0}
	}

	envelope := make([]float64, numHops)
	for i := range envelope {
		start := i * hopSamples
		end := start + hopSamples
		if end > n {
			end = n
		}
		sum := 0.0
		for _, s := range mono.Samples[start:end] {
			sum += s * s
		}
		envelope[i] = math.Sqrt(sum / float64(end-start))
	}

	minLoopHops := int(math.Ceil(minLoopSec / hopSec))
	maxLoopHops := numHops
	if maxLoopSec > 0 {
		maxLoopHops = int(math.Floor(maxLoopSec / hopSec))
	}
	if minLoopHops >= maxLoopHops {
		return &LoopResult{Start: 0, End: totalDur, Length: totalDur, Correlation: 0}
	}

	// Comparison window: 5 seconds worth of hops (10 hops).
	winHops := 10
	if winHops > numHops/10 {
		winHops = numHops / 10
	}
	if winHops < 2 {
		winHops = 2
	}

	// Try 10 loop-end candidates spread from 80% to 98% of the song.
	// For each, scan all valid loop-start positions and pick the best
	// (start, end) pair by score = 0.4*correlation + 0.6*lengthFraction.
	// Weighting length at 60% ensures we strongly prefer long loops.
	const numEndCandidates = 10
	bestScore := -math.MaxFloat64
	bestStartSample := 0
	bestEndSample := n
	bestCorr := 0.0

	totalIter := numEndCandidates * numHops
	progressStep := totalIter / 20
	if progressStep < 1 {
		progressStep = 1
	}
	progressCount := 0

	for ei := 0; ei < numEndCandidates; ei++ {
		fraction := 0.80 + float64(ei)/float64(numEndCandidates-1)*0.18
		endHop := int(fraction * float64(numHops))
		if endHop+winHops > numHops {
			endHop = numHops - winHops
		}
		if endHop < winHops {
			continue
		}
		endWin := envelope[endHop : endHop+winHops]
		maxStartHop := endHop - minLoopHops
		if maxStartHop < 0 {
			continue
		}

		for startHop := 0; startHop <= maxStartHop; startHop++ {
			progressCount++
			if progressReporter != nil && progressCount%progressStep == 0 {
				pct := float64(progressCount) / float64(totalIter) * 100
				progressReporter(pct, "Scanning loop points")
			}
			if startHop+winHops > numHops {
				break
			}
			corr := pearsonCorr(envelope[startHop:startHop+winHops], endWin)

			loopHops := endHop - startHop
			lengthFrac := float64(loopHops) / float64(maxLoopHops)
			if lengthFrac > 1.0 {
				lengthFrac = 1.0
			}
			score := corr*0.4 + lengthFrac*0.6

			if score > bestScore {
				bestScore = score
				bestStartSample = startHop * hopSamples
				bestEndSample = endHop * hopSamples
				bestCorr = corr
			}
		}
	}

	startDur := time.Duration(float64(bestStartSample) / float64(sr) * float64(time.Second))
	endDur := time.Duration(float64(bestEndSample) / float64(sr) * float64(time.Second))
	return &LoopResult{
		Start:       startDur,
		End:         endDur,
		Length:      endDur - startDur,
		Correlation: bestCorr,
	}
}

// pearsonCorr returns the Pearson correlation coefficient of two equal-length slices.
// Returns 0 if either slice has zero variance.
func pearsonCorr(a, b []float64) float64 {
	n := len(a)
	if n == 0 || n != len(b) {
		return 0
	}
	var ma, mb float64
	for i := range a {
		ma += a[i]
		mb += b[i]
	}
	ma /= float64(n)
	mb /= float64(n)
	var sa, sb, cov float64
	for i := range a {
		da := a[i] - ma
		db := b[i] - mb
		sa += da * da
		sb += db * db
		cov += da * db
	}
	denom := math.Sqrt(sa * sb)
	if denom == 0 {
		return 0
	}
	return cov / denom
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
// crossfade of crossfadeMs milliseconds at each loop junction, and a
// linear fade-out of fadeOutMs milliseconds at the very end.
func extendAudio(pcm []byte, sampleRate int, loop *LoopResult, targetDur time.Duration, crossfadeMs, fadeOutMs int) []byte {
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

	// Apply linear fade-out to the last fadeOutMs milliseconds.
	if fadeOutMs > 0 {
		fadeFrames := fadeOutMs * sampleRate / 1000
		totalOut := len(out) / bytesPerFrame
		if fadeFrames > totalOut {
			fadeFrames = totalOut
		}
		fadeStart := totalOut - fadeFrames
		for i := 0; i < fadeFrames; i++ {
			alpha := 1.0 - float64(i)/float64(fadeFrames) // 1→0
			fOff := (fadeStart + i) * bytesPerFrame
			for ch := 0; ch < 2; ch++ {
				idx := fOff + ch*2
				val := int16(binary.LittleEndian.Uint16(out[idx : idx+2]))
				binary.LittleEndian.PutUint16(out[idx:idx+2], uint16(int16(float64(val)*alpha)))
			}
		}
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
