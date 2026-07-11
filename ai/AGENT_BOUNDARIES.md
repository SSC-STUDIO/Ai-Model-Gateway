# AI Model Gateway Agent Boundaries

- Allowed: internal, configs, docs, ai, and directly related tests.
- Never perform Git history or worktree cleanup operations; leave changes for master review.
- Never print, copy, or persist real provider credentials.
- Never transparently retry after response bytes have reached the client.
- Never add infinite retries or an unbounded queue.
- Do not modify unrelated website, promotion, or release content during reliability work.

## Controlled publication
- During development revisions, do not commit or push.
- After the fixed Hermes master approves the evidence, the same Worker Session must stage only the approved files, create one normal commit, and push the current branch to its configured upstream.
- Never use `git add .`, `git add -A`, force-push, amend, rebase, reset, clean, checkout, stash, or branch switching.
- If the upstream is missing, authentication fails, or the working tree differs from approved evidence, stop publication and report the exact blocker.

## Task plan gate
- Each task revision owns exactly one detailed file under `ai/task-plans/`. Create it once before source edits, then reuse and update it for all feedback attempts of that revision; never create copies.
- The plan must contain `# Task Plan`, `## Goal`, `## Baseline`, `## Scope`, `## Steps`, `## Verification`, `## Risks`, `## Stop Conditions`, and `## Evidence`, and must be at least 800 UTF-8 bytes.
- Update the same task plan with actual commands, findings, deviations, and final evidence. Generic or retrospective-only plans fail review.
- The task plan is part of the approved task changes and is committed and pushed with the implementation after master review.
