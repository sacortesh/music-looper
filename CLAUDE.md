# Harness Instructions

This project uses the Avangarde Harness. Before doing anything:

1. Read `harness/loops/tasks.md` — find the current task and branch.
2. Read `harness/guides/` — architecture rules, coding rules, AGENTS.md.
3. Do not code outside the current task scope.
4. Run `harness/sensors/check.sh` after every change.

## Available commands

```
avangardespec pre-task   → before coding a new task (BDD spec + branch)
avangardespec task       → start the Ralph loop (AI coding + sensors)
avangardespec close      → close task after sensors pass
avangardespec scope      → add a new task or feature mid-project
```

## Base branch: main

