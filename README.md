# music-loop

A CLI tool that extends MP3 files into seamless, long-duration loops — perfect for background music, game soundtracks, lo-fi listening sessions, or video production.

It analyzes the audio to find the best loop points automatically: the intro plays once, then the repeating body loops for as long as you need, with smooth crossfade transitions and a fade-out at the end.

## Features

- **Automatic loop detection** — finds natural loop start/end points using energy envelope analysis, strongly favouring longer loops
- **Intro-aware** — intro plays once, then the loop body repeats seamlessly
- **Crossfade** — smooth blending at each loop junction (configurable)
- **Fade-out** — linear fade at the end of the output (configurable)
- **Batch mode** — process an entire directory of MP3s in one command
- **Dry-run mode** — analyze loop points without writing any output

## Installation

Requires [Go](https://golang.org/) 1.21+ and [LAME](https://lame.sourceforge.io/) (`brew install lame` on macOS).

```bash
git clone https://github.com/skraheux/music-loop
cd music-loop
go build -o music-loop .
```

## Usage

### Single file

```bash
music-loop [flags] <input.mp3> <target-minutes>
```

```bash
# Extend a track to 60 minutes with default settings
music-loop song.mp3 60

# Custom output path
music-loop -output song_extended.mp3 song.mp3 60

# Smoother transitions and longer fade-out
music-loop --crossfade 500 --fade-out 5000 song.mp3 60

# Dry run — analyze only, no output written
music-loop --dry-run song.mp3 60
```

### Batch mode

```bash
# Process all MP3s in a directory, outputs alongside source files
music-loop ./music/ 60

# Write outputs to a separate directory
music-loop --output ./output/ ./music/ 60
```

### All flags

| Flag | Default | Description |
|------|---------|-------------|
| `--output` | `<input>_loop.mp3` | Output file or directory |
| `--min-loop` | `10.0` | Minimum loop duration in seconds |
| `--max-loop` | `0` | Maximum loop duration in seconds (0 = full track) |
| `--crossfade` | `50` | Crossfade duration at loop junctions (ms) |
| `--fade-out` | `2000` | Fade-out duration at the end of output (ms, 0 to disable) |
| `--dry-run` | `false` | Analyze only, do not write output |
| `--verbose` | `false` | Print detailed progress information |

### Tips

- If the loop transition sounds abrupt, increase `--crossfade` (try `500`) and `--fade-out` (try `5000`)
- Use `--dry-run --verbose` to inspect detected loop points before committing to a full encode
- Songs with a distinct intro that differs from the main body loop best — the tool is designed for exactly that structure

## How it works

1. Decodes the MP3 to raw PCM
2. Converts to mono and downsamples to 11025 Hz for fast analysis
3. Computes an energy envelope (RMS per 500ms window)
4. Searches for the best `(loop_start, loop_end)` pair by scoring candidates on both similarity (Pearson correlation of 5-second energy windows) and loop length — favouring long loops
5. Extends the audio: intro plays once, loop body repeats with crossfade until the target duration is reached, then a fade-out is applied
6. Re-encodes to MP3

## Support

If this tool saved you time or you just like it, consider buying me a coffee!

[![ko-fi](https://ko-fi.com/img/githubbutton_sm.svg)](https://ko-fi.com/Y8Y81MNWEN)

Issues and pull requests are welcome.

## License

MIT
