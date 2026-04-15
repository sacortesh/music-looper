Feature: Loop detection via autocorrelation

  Scenario: Detect repeating loop in audio
    Given a decoded mono signal with a repeating pattern
    When autocorrelation analysis is run with a minimum loop of 10 seconds
    Then the detected loop start, end, and duration are reported
    And the correlation score indicates a strong match (> 0.8)

  Scenario: Track too short for minimum loop
    Given a mono signal shorter than the minimum loop duration
    When autocorrelation analysis is run
    Then the entire track is returned as the loop with correlation 0

  Scenario: CLI displays loop detection results
    Given a valid MP3 file "sample1.mp3"
    When the tool processes it with a target duration
    Then the output includes "Loop start", "Loop end", "Loop length", and "Correlation"
