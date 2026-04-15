Feature: CLI skeleton with MP3 decoding

  Scenario: Display audio stats for a valid MP3 file
    Given an MP3 file "test.mp3"
    When I run `music-loop test.mp3 10`
    Then the output shows sample rate, channels, duration, sample count, and target minutes

  Scenario: Error on missing arguments
    When I run `music-loop` with no arguments
    Then it exits with an error and prints usage instructions

  Scenario: Error on invalid MP3 file
    Given a file "bad.txt" that is not a valid MP3
    When I run `music-loop bad.txt 5`
    Then it exits with an error mentioning the decode failure
