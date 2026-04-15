Feature: Convert stereo PCM to mono float64 for analysis

  Scenario: Stereo PCM is converted to mono and downsampled
    Given a decoded MP3 with stereo 16-bit PCM at 44100 Hz
    When the PCM is converted to mono at 11025 Hz
    Then the output has approximately 1/4 the original sample count
    And all sample values are in the range [-1.0, 1.0]

  Scenario: CLI displays mono signal stats
    Given a valid MP3 file "sample1.mp3"
    When the user runs music-loop with "sample1.mp3 5"
    Then the output includes "Mono signal:" with sample count and rate
