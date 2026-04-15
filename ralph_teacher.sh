#!/bin/bash

claude --permission-mode acceptEdits "@PRD.md @progress.md \
1. Read the PRD and progress file. \
2. Find the next incomplete task and ask client to implement it with clear directives, as if teaching. \
3. Prompt the user to ask when task has been implemented. \
4. Perform diff and analyse implementation. \
5. Provide feedback to user, on how to do better, what to dedicate \
time into getting better, what is technical debt, etc. \
6. Commit changes. \
7. Update PRD with additional tasks. \
8. Update progress.txt with what we achieved. \
WE ONLY DO ONE TASK AT A TIME."