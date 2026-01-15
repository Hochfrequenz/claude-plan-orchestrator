---
name: autonomous-plan-execution
description: Use when executing implementation plans that should complete fully autonomously with automatic PR creation and merge, no user interaction needed
---

# Autonomous Plan Execution

## Overview

Load plan, review critically, execute all tasks, automatically create PR and merge.

**Core principle:** Fully autonomous execution with no user interaction.

**Announce at start:** "I'm using the autonomous-plan-execution skill to implement this plan autonomously."

## The Process

### Step 1: Load and Review Plan
1. Read plan file
2. Review critically - identify any questions or concerns about the plan
3. If concerns: Raise them with your human partner before starting
4. If no concerns: Create TodoWrite and proceed

### Step 2: Execute Batch
**Default: First 3 tasks**

For each task:
1. Mark as in_progress
2. Follow each step exactly (plan has bite-sized steps)
3. Run verifications as specified
4. Mark as completed

### Step 3: Report
When batch complete:
- Show what was implemented
- Show verification output
- Say: "Ready for feedback."

### Step 4: Continue
Based on feedback:
- Apply changes if needed
- Execute next batch
- Repeat until complete

### Step 5: Complete Development (Autonomous)

After all tasks complete and verified:

1. **Verify tests pass:**
   ```bash
   # Run project's test suite
   npm test / cargo test / pytest / go test ./...
   ```
   If tests fail: Fix issues, re-run. Do NOT proceed until tests pass.

2. **Commit all changes:**
   ```bash
   git add -A
   git commit -m "feat: [descriptive message]

   Co-Authored-By: Claude <noreply@anthropic.com>"
   ```

3. **Push branch:**
   ```bash
   git push -u origin HEAD
   ```

4. **Create Pull Request:**
   ```bash
   gh pr create --title "[Feature Title]" --body "$(cat <<'EOF'
   ## Summary
   - [Bullet points of what was implemented]

   ## Test Plan
   - All tests pass
   - [Any specific verification steps]

   Generated with Claude Code
   EOF
   )"
   ```

5. **Merge the PR:**
   ```bash
   gh pr merge --squash --delete-branch
   ```

6. **Cleanup worktree (if applicable):**
   ```bash
   # If in a worktree, remove it
   git worktree list | grep $(pwd) && cd .. && git worktree remove <worktree-path>
   ```

## When to Stop and Ask for Help

**STOP executing immediately when:**
- Hit a blocker mid-batch (missing dependency, test fails, instruction unclear)
- Plan has critical gaps preventing starting
- You don't understand an instruction
- Verification fails repeatedly
- Tests fail after multiple fix attempts

**Ask for clarification rather than guessing.**

## When to Revisit Earlier Steps

**Return to Review (Step 1) when:**
- Partner updates the plan based on your feedback
- Fundamental approach needs rethinking

**Don't force through blockers** - stop and ask.

## Remember
- Review plan critically first
- Follow plan steps exactly
- Don't skip verifications
- Reference skills when plan says to
- Between batches: just report and wait
- Stop when blocked, don't guess
- Tests MUST pass before PR creation
- Automatically merge PR at the end

## Key Difference from executing-plans

This skill does NOT use the finishing-a-development-branch skill. Instead, it automatically:
1. Creates PR
2. Merges with --squash --delete-branch
3. Cleans up worktree

No user interaction required for the final steps.
