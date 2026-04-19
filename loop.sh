#!/bin/bash
# loop.sh — wrapper for the music-loop binary
#
# Usage:
#   ./loop.sh <input.mp3|dir> <target-minutes> [options]
#
# Options:
#   -o, --output <path>       output file or directory (default: <input>_loop.mp3)
#   -m, --min-loop <secs>     minimum loop duration in seconds (default: 10)
#   -M, --max-loop <secs>     maximum loop duration in seconds (default: half track)
#   -c, --crossfade <ms>      crossfade duration in milliseconds (default: 50)
#   -f, --fade-out <ms>       fade-out duration in milliseconds at end (default: 2000, 0 to disable)
#   -d, --dry-run             analyze only, do not write output
#   -v, --verbose             print detailed progress
#
# Examples:
#   ./loop.sh song.mp3 30
#   ./loop.sh song.mp3 60 --verbose
#   ./loop.sh song.mp3 45 --min-loop 20 --crossfade 100 --output out.mp3
#   ./loop.sh song.mp3 30 --fade-out 3000
#   ./loop.sh song.mp3 10 --dry-run
#   ./loop.sh ./tracks/ 30
#   ./loop.sh ./tracks/ 30 --output ./looped/

set -euo pipefail

BINARY="$(dirname "$0")/music-loop"

if [ ! -x "$BINARY" ]; then
    echo "Error: binary not found at $BINARY" >&2
    echo "Build it first with: go build -o music-loop ." >&2
    exit 1
fi

if [ $# -lt 2 ]; then
    echo "Usage: ./loop.sh <input.mp3|dir> <target-minutes> [options]"
    echo ""
    echo "Options:"
    echo "  -o, --output <path>     output file or directory (default: <input>_loop.mp3)"
    echo "  -m, --min-loop <secs>   minimum loop duration in seconds (default: 10)"
    echo "  -M, --max-loop <secs>   maximum loop duration in seconds (default: half track)"
    echo "  -c, --crossfade <ms>    crossfade in milliseconds (default: 50)"
    echo "  -f, --fade-out <ms>     fade-out at end in milliseconds (default: 2000, 0 to disable)"
    echo "  -d, --dry-run           analyze only, no output written"
    echo "  -v, --verbose           detailed progress output"
    exit 1
fi

INPUT="$1"
TARGET="$2"
shift 2

# Translate short flags to Go flag equivalents
ARGS=()
while [ $# -gt 0 ]; do
    case "$1" in
        -o|--output)    ARGS+=("--output=$2");    shift 2 ;;
        -m|--min-loop)  ARGS+=("--min-loop=$2");  shift 2 ;;
        -M|--max-loop)  ARGS+=("--max-loop=$2");  shift 2 ;;
        -c|--crossfade) ARGS+=("--crossfade=$2"); shift 2 ;;
        -f|--fade-out)  ARGS+=("--fade-out=$2");  shift 2 ;;
        -d|--dry-run)   ARGS+=("--dry-run");      shift   ;;
        -v|--verbose)   ARGS+=("--verbose");      shift   ;;
        *) echo "Unknown option: $1" >&2; exit 1 ;;
    esac
done

exec "$BINARY" ${ARGS[@]+"${ARGS[@]}"} "$INPUT" "$TARGET"
