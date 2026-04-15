Feature: Performance optimization and progress reporting

  Scenario: Progress indicator shown in verbose mode
    Given a valid MP3 file "sample1.mp3"
    When the user runs music-loop --verbose sample1.mp3 2
    Then stderr contains progress lines like "[progress] Scanning autocorrelation..."
    And stderr contains progress lines like "[progress] Extending audio..."

  Scenario: FFT-based loop detection runs without timeout
    Given a valid MP3 file "sample1.mp3"
    When the user runs music-loop --dry-run --verbose sample1.mp3 5
    Then the loop detection completes within a reasonable time
    And stderr contains "Loop detection completed in"
