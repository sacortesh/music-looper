Feature: MP3 encoding and passthrough round-trip

  Scenario: Decode then re-encode produces a valid MP3
    Given an MP3 file "sample1.mp3"
    When I run `music-loop sample1.mp3 10`
    Then an output file "sample1_loop.mp3" is created
    And the output file is a valid MP3 with the same sample rate as the input

  Scenario: Output file is non-empty
    Given an MP3 file "sample1.mp3"
    When I run `music-loop sample1.mp3 5`
    Then "sample1_loop.mp3" has a file size greater than zero
