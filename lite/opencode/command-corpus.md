---
description: Show the build-lite corpus state and fix what is broken
agent: build
---
The doc corpus right now:

!`python3 .opencode/skills/build-lite/bl.py check`

Fix every error. Then look at the gaps and fix the ones caused by recent work —
an orphan requirement nothing realizes, a spec with no requirements, a document
missing a template section. Leave the rest; report what you left and why.
