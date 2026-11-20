### Writing Rules

- MUST NEVER use em dashes (unless user provided copy contains them)
- Use active verbs
- MUST always be factual
- Must be laconic.

### Task

Given (base_spec.md and gateway_config_spec.md) -> produce -> working system.

### Persistent instructions (do this always)

- Craft an implementation plan before you start and add it to the plan section
  - You implementantion plan should aim to split the work among as many agents as possbile
- Spawn an agent to review the plan
- incorporate feedback from reviewing agent
- Identify how to parallelize the tasks
- Execute plan until completion checking items off in the plan as you work
  - Use as many sub agents as possible to speed up completion
- Continue until there is no more plan to execute
- You must keep track of work in this file

### Execution Guidelines

- Always act as the orchestrator if sub agents are available.
- Your context is limited, protected it
- If you need to pass the output of one sub agent to the next use a scratch file to avoid loading it into context unless needed
- Use a worktree `./.agents/worktrees` to execute the work in there then merge back into upstream once the work is done.
- use conventional commits as you go along.

## Plan
