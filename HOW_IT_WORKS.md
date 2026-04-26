# How It Works

A visual walkthrough of the audio analysis and loop detection pipeline.

---

## Step 1 — Decode the MP3

The MP3 is decoded into raw **PCM** (Pulse-Code Modulation) — the actual numbers
representing the air pressure of the sound wave, thousands of times per second.

```
MP3 file  →  decoder  →  PCM samples (stereo, 44100 Hz)

                           L R  L R  L R  L R  L R  L R ...
samples:                  [█▓][█░][▓█][░█][██][░░][▓▓] ...
                           ↑
                      each pair = one stereo frame
                      44100 frames per second
```

---

## Step 2 — Convert to Mono + Downsample

Full stereo at 44100 Hz is overkill for *analysis* (not playback). We:

1. **Average L + R** → mono (one number per frame)
2. **Downsample** from 44100 → 11025 Hz (keep 1 in every 4 samples)

```
Stereo 44100 Hz:   L R  L R  L R  L R  L R  L R  L R  L R
                    ↓    ↓    ↓    ↓    ↓    ↓    ↓    ↓
Mono average:       M    M    M    M    M    M    M    M
                    ↓         ↓         ↓         ↓
Decimated 11025:    M         M         M         M

Result: 4× fewer numbers to work with — same musical content, less data.
```

The **original stereo PCM** (44100 Hz) is kept untouched for the final output.
The mono 11025 Hz signal is only used for analysis.

---

## Step 3 — Energy Envelope

Raw samples are noisy and hard to compare directly. Instead, we measure how
**loud** the signal is every 500ms. This is called the **energy envelope**.

Each 500ms chunk of samples becomes a single number: the **RMS** (root mean square),
which is essentially the average loudness of that window.

```
Raw mono signal (zoomed in on 4 seconds):

amplitude
   │  ╭╮  ╭─╮    ╭╮ ╭╮
   │ ╭╯╰╮╭╯ ╰╮  ╭╯╰─╯╰╮
   │╭╯  ╰╯   ╰╮╭╯      ╰╮
───┼────────────────────────→ time
   │                         4 seconds

Group into 500ms windows:
   [  win 1  ][  win 2  ][  win 3  ][  win 4  ][  win 5  ][  win 6  ][  win 7  ][  win 8  ]

Compute RMS of each window:
   0.6         0.3         0.7         0.8         0.4         0.5         0.2         0.6

Energy envelope:
   █           ░           ██          ██          ▓           █           ░           █
   0.6         0.3         0.7         0.8         0.4         0.5         0.2         0.6
```

For a 143-minute song at 11025 Hz that's ~94 million samples.
After the envelope: just **17,160 numbers** (one per 500ms). Much more manageable.

---

## Step 4 — Loop Point Detection

This is the core of the algorithm. The goal is to find two time positions:

```
┌─────────────────────────────────────────────────────────────────────┐
│                         Full song                                   │
│                                                                     │
│  [    intro    ][          loop body                    ]           │
│  0         loop_start                              loop_end         │
│                                                                     │
│  • intro plays ONCE                                                 │
│  • loop body repeats until target duration                          │
└─────────────────────────────────────────────────────────────────────┘
```

### How the search works

We try **10 candidate loop-end positions** spread from 80% to 98% of the song.
For each end candidate, we slide a **comparison window** across all earlier positions
in the song to find where the audio sounds most similar.

```
Song (as energy envelope, condensed):

position:  0%                                                    100%
           ├────────────────────────────────────────────────────────┤
envelope:  ▓░▓█░░▓▓█░▓░░█▓▓░░▓█░▓░░▓▓█░░▓░▓░░▓░▓█░░▓▓█░▓░░█▓▓░░▓█

                                                    ↑ end candidate (e.g. 88%)

           Compare a 5-second window here:          [████░▓▓░░▓]
                                                     "end window"

           ...against every earlier 5-second window:

           [▓░▓█░▓▓█░] at 0%    → similarity = 0.2
           [░▓░░█▓▓░░] at 5%    → similarity = 0.5
           [▓█░▓░░▓▓█] at 10%   → similarity = 0.1
           ...
           [░▓▓█░░▓░▓] at 62%   → similarity = 0.8  ← best match
           ...

           Best match at 62% → loop_start = 62%
           Loop body = 62% to 88% = 26% of song
```

### Similarity score: Pearson correlation

We compare the **shape** of the two 5-second energy windows using Pearson correlation:

```
End window:    [0.6, 0.3, 0.7, 0.8, 0.4, 0.5, 0.2, 0.6, 0.7, 0.3]
Candidate:     [0.5, 0.4, 0.8, 0.7, 0.3, 0.6, 0.3, 0.5, 0.8, 0.2]

Pearson ≈ 0.95  ← high: both windows rise and fall in the same pattern
                  (the exact loudness level doesn't matter, only the shape)

Pearson ≈ 0.1   ← low: the patterns don't match — different musical section
Pearson ≈ -0.8  ← negative: opposite pattern — one loud while other is quiet
```

### Final score: correlation vs. length

A perfect 5-second match with a short loop is worse than a good match with a long loop.
We score every `(start, end)` candidate with:

```
score = (0.4 × correlation) + (0.6 × loop_length_fraction)
          ↑                          ↑
          quality                    length as fraction of max possible

Examples (143-minute song):

  start=0s,   end=17s   → score = 0.4×0.90 + 0.6×(17/8580)   = 0.36 + 0.001 = 0.361
  start=30m,  end=130m  → score = 0.4×0.65 + 0.6×(100/143)   = 0.26 + 0.42  = 0.680 ✓
  start=10m,  end=120m  → score = 0.4×0.70 + 0.6×(110/143)   = 0.28 + 0.46  = 0.740 ✓✓

The algorithm picks the highest score → always prefers long loops.
```

---

## Step 5 — Extend the Audio

With `loop_start` and `loop_end` found, we build the output:

```
Output timeline (target = 60 minutes):

├──[intro]──┤──[  loop body  ]──┬──[  loop body  ]──┬──[  loop body  ]──┤
0      loop_start          loop_end             loop_end             loop_end
                                ↑                    ↑
                           crossfade            crossfade
                           blends the           blends the
                           junction             junction

Last few seconds:
                                                              └──[fade out]┘
                                                               volume: 1.0 → 0.0
```

### Crossfade detail

At each junction, instead of a hard cut, we blend the outgoing tail
with the incoming head over `--crossfade` milliseconds (default 50ms):

```
Outgoing: ──────────────╲
                          ╲  ← volume fades from 1.0 to 0.0
Incoming:            ╱────────────
                    ╱  ← volume fades from 0.0 to 1.0

Blended:  ────────────────────────  ← smooth transition
```

---

## Summary

```
MP3
 │
 ▼
PCM (stereo, 44100 Hz)  ←── kept for final output
 │
 ▼
Mono 11025 Hz  ←── analysis only
 │
 ▼
Energy envelope (one number per 500ms)  ←── 17k values for a 143-min song
 │
 ▼
Loop point search:
  • 10 end candidates (80–98% of song)
  • Compare 5s window at end vs. all earlier positions
  • Score = 0.4×similarity + 0.6×length
  • Pick best (loop_start, loop_end)
 │
 ▼
Extend audio:
  intro (once) + [loop body × N] + fade out
 │
 ▼
MP3 output
```
