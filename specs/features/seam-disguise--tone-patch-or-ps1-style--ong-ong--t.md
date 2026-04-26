# Task Spec: Seam disguise: tone patch or PS1-style "ong ong" transient layered at loop point

Slug: seam-disguise--tone-patch-or-ps1-style--ong-ong--t
Branch: task/seam-disguise--tone-patch-or-ps1-style--ong-ong--t
Created: 2026-04-26

---

## BDD Acceptance Criteria

Feature: Seam disguise at loop point

  Scenario: PS1-style transient layered at loop seam
    Given an MP3 extended with at least one loop repetition
    When the `--seam-disguise ong` flag is provided
    Then a short transient sound (≤ 200ms, resembling a PS1 disc-reload click) is mixed into the audio at each loop boundary

  Scenario: Tone patch layered at loop seam
    Given an MP3 extended with at least one loop repetition
    When the `--seam-disguise tone` flag is provided
    Then a brief sine-wave tone burst (≤ 100ms, amplitude ≤ -12dBFS relative to track peak) is mixed at each loop boundary

  Scenario: Seam disguise absent when flag is omitted
    Given an MP3 extended with at least one loop repetition
    When no `--seam-disguise` flag is provided
    Then the output audio contains no injected transient or tone at loop boundaries and behaviour matches prior crossfade-only output

  Scenario: Dry-run reports seam disguise type without writing file
    Given a valid MP3 input and `--dry-run` combined with `--seam-disguise ong`
    When the CLI executes
    Then stdout reports loop boundary timestamps and seam disguise type ("ong") and no output file is written

---



## Notes

<!-- Human notes appended here during execute-task.sh iterations -->
