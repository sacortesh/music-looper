# Lessons & Decisions

> Claude reads this at the start of every iteration.
> It records key decisions, user feedback, and technical learnings accumulated
> across sessions so context is never lost.

## Decisions
- `go-mp3` always decodes to stereo 16-bit PCM (4 bytes per sample frame). Channels=2 is hardcoded based on library behavior.
- `go-lame` encoder quality set to 2 (near-best quality, reasonable speed). CGO requires `libmp3lame` — installed via `brew install lame` at `/usr/local/Cellar/lame/3.100`.

## Feedback
_No feedback recorded yet._

## Learnings
- Go is installed at `/usr/local/go/bin/go` (v1.26.2) but not on default PATH — needs explicit PATH export.
- There is an mp3 called `sample1.mp3`
