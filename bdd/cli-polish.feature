Feature: CLI polish and validation

  Scenario: Missing arguments shows usage
    When the user runs music-loop with no arguments
    Then stderr contains "Usage:" and the process exits with code 1

  Scenario: Non-MP3 input is rejected
    Given a file "track.wav" exists
    When the user runs music-loop track.wav 5
    Then stderr contains "must have .mp3 extension" and the process exits with code 1

  Scenario: Dry-run prints analysis without writing output
    Given a valid MP3 file "sample1.mp3"
    When the user runs music-loop --dry-run sample1.mp3 5
    Then the output includes loop detection stats
    And the output contains "Dry run" and no output file is created
