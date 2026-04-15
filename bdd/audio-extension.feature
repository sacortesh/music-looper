Feature: Audio extension by loop repetition
  The tool extends an MP3 to a target duration by repeating the detected loop.

  Scenario: Extend a short track to double its length
    Given an MP3 file "sample1.mp3" with a detected loop
    When the user runs the tool with target duration 2x the original length
    Then the output MP3 is approximately the target duration
    And the loop junctions have a smooth crossfade with no audible clicks

  Scenario: Target shorter than original track
    Given an MP3 file "sample1.mp3"
    When the user runs the tool with a target shorter than the track
    Then the output contains at least one full loop iteration

  Scenario: Crossfade blending at loop boundaries
    Given a repeating audio loop detected in the input
    When the loop is repeated to reach the target duration
    Then each junction blends 50ms of the tail with 50ms of the head
    And the blended samples remain within valid PCM range
