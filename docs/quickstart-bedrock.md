# Quickstart — AWS Bedrock

Run the MAPLE squad against Anthropic models hosted on **AWS Bedrock** instead of the
public Anthropic API. Works with both **Claude Code** and **OpenCode** harnesses; no
MAPLE binary changes required — MAPLE scaffolds the configuration, the harness talks
to the provider.

## TL;DR

```bash
cd your-project-directory
maple init --provider bedrock
source scripts/maple/provider.env    # Claude Code only
claude .   # or: opencode .
```

`maple init --provider bedrock` does all of the following in one shot:

- Patches `opencode.json` with the `amazon-bedrock` provider block.
- Sets the per-agent `model:` frontmatter in `.opencode/agents/*.md` and
  `.claude/agents/*.md` to Bedrock model IDs (Opus tier for `orchestrator` /
  `architect`, Sonnet tier for implementation agents, Haiku tier for `docs` /
  `rubber-duck`).
- Writes `scripts/maple/provider.env` with the Claude Code env vars
  (`CLAUDE_CODE_USE_BEDROCK=1`, `AWS_REGION`, `ANTHROPIC_MODEL`,
  `ANTHROPIC_SMALL_FAST_MODEL`).
- Records the choice in `.maple/provider.json` so subsequent `maple update` runs
  re-apply it without needing the flag again.

To pin every agent to one specific model:

```bash
maple init --provider bedrock \
  --model amazon-bedrock/us.anthropic.claude-sonnet-4-6
```

The rest of this doc covers the underlying AWS setup and per-harness details for
when you want to dig deeper or troubleshoot.

---

## 1. AWS prerequisites

### IAM permissions

The identity you run the harness as needs at minimum:

```json
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Action": [
      "bedrock:InvokeModel",
      "bedrock:InvokeModelWithResponseStream",
      "bedrock:Converse",
      "bedrock:ConverseStream"
    ],
    "Resource": "*"
  }]
}
```

For cross-region inference profiles (recommended for Claude 3.5/3.7/4), also allow
`bedrock:GetInferenceProfile` and `bedrock:ListInferenceProfiles`.

### Enable model access

In the AWS Console → Bedrock → **Model access**, request access to the Anthropic
Claude models you intend to use. Approval is usually instant for Claude 3.x and 4.x
families. Common picks for MAPLE:

| MAPLE role | Bedrock model ID |
|---|---|
| `orchestrator`, `architect` | `us.anthropic.claude-opus-4-8` |
| Implementation agents | `us.anthropic.claude-sonnet-4-6` |
| Lightweight (`docs`, `rubber-duck`) | `us.anthropic.claude-haiku-4-5-20251001-v1:0` |

> The `us.` / `eu.` prefix denotes a **cross-region inference profile**. Use those IDs
> for Claude 3.5+ in `us-east-1` / `us-west-2` / `eu-west-1` etc. Plain model IDs
> (without the prefix) only work in the model's home region.

### Credentials

Export standard AWS env vars before launching the harness — same shell that runs
`claude` or `opencode`:

```bash
export AWS_REGION=us-east-1
export AWS_PROFILE=my-bedrock-profile     # OR use AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY
```

Verify with `aws bedrock list-foundation-models --region "$AWS_REGION"`.

---

## 2. Claude Code → Bedrock

`maple init --provider bedrock` writes the four required env vars to
`scripts/maple/provider.env`:

```bash
export CLAUDE_CODE_USE_BEDROCK=1
export AWS_REGION=${AWS_REGION:-us-east-1}
export ANTHROPIC_MODEL=us.anthropic.claude-sonnet-4-6
export ANTHROPIC_SMALL_FAST_MODEL='us.anthropic.claude-haiku-4-5-20251001-v1:0'
```

> The `ANTHROPIC_MODEL` / `ANTHROPIC_SMALL_FAST_MODEL` values are derived from the
> resolved per-tier models — `maple init --provider bedrock --model <id>` writes that
> `<id>` into both. `AWS_REGION` uses a `${AWS_REGION:-…}` default so sourcing the file
> never clobbers a region you already exported.

Source it (or copy the lines into your shell profile / direnv `.envrc`):

```bash
source scripts/maple/provider.env
claude .
```

The Claude Code status line should show the Bedrock model ID after launch.

### Per-agent overrides (Claude Code)

Claude Code's `.claude/agents/*.md` files use the inherited shell model by default.
`maple init --provider bedrock` also injects `model:` into each Claude Code agent
file. To deviate for one agent, set it to any value whose prefix is not one of
MAPLE's known providers — hand-set values are preserved on update.

---

## 3. OpenCode → Bedrock

`maple init --provider bedrock` adds this block to `opencode.json`:

```json
{
  "provider": {
    "amazon-bedrock": {
      "options": { "region": "{env:AWS_REGION}" }
    }
  },
  "model": "amazon-bedrock/us.anthropic.claude-sonnet-4-6"
}
```

Credentials come from the standard AWS credential chain (env vars, `~/.aws/credentials`,
SSO, instance role) — nothing is stored in `opencode.json`.

```bash
export AWS_PROFILE=your-profile
export AWS_REGION=us-east-1
opencode .
```

### Per-agent overrides

`maple init --provider bedrock` already assigns tier-appropriate models to every
agent. If you want to deviate for one agent, edit its `model:` frontmatter. Two cases
are preserved on a `maple update` (replay of the persisted choice):

- A value with **no** known-provider prefix (e.g. an inference-profile ARN) — never touched.
- A value pointing at a **different** managed provider (e.g. one agent on
  `github-copilot/…` while the project is on `amazon-bedrock`) — kept on update.

Only an explicit `maple init --provider <name>` re-switch rewrites every
managed `model:` line to the new provider.

```yaml
# .opencode/agents/orchestrator.md
model: arn:aws:bedrock:us-east-1:123456789012:application-inference-profile/abc123
```

MAPLE-managed defaults per tier:

| Tier | Agents | Bedrock model ID |
|---|---|---|
| deep | `orchestrator`, `architect` | `us.anthropic.claude-opus-4-8` |
| fast | everything else | `us.anthropic.claude-sonnet-4-6` |
| small | `docs`, `rubber-duck`, `humanizer`, `spec-kit` | `us.anthropic.claude-haiku-4-5-20251001-v1:0` |

---

## 4. Mixing providers

Both harnesses let you mix providers per-agent. A common pattern:

- `orchestrator`, `architect` → Bedrock Opus (auditable, in your AWS account)
- High-volume implementation agents → `github-copilot/claude-sonnet-4.5` (cheaper)
- Specialist agents that need long context → Bedrock Sonnet

In OpenCode, list every provider you use in the `provider` block of `opencode.json`;
each agent's `model:` field selects which one runs that agent.

---

## 5. Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `AccessDeniedException: You don't have access to the model` | Model access not approved | AWS Console → Bedrock → Model access |
| `ValidationException: Invocation of model ID … isn't supported` | Using a base model ID outside its home region | Switch to the `us.` / `eu.` inference-profile ID |
| `ThrottlingException` repeatedly | Account-level TPM limit | Request a quota increase, or fan out fewer parallel agents |
| Claude Code still hits api.anthropic.com | `CLAUDE_CODE_USE_BEDROCK` not exported in the shell that launched `claude` | Re-export and restart the terminal |
| OpenCode error `provider amazon-bedrock not found` | Old OpenCode version | `npm install -g opencode-ai@latest` |
| `ExpiredTokenException` | SSO session expired | `aws sso login --profile <profile>` |

### Verifying the wiring

From inside the harness, ask the agent:

> "What model are you running on? Print your model ID exactly."

Both Claude Code and OpenCode surface the model ID in their status line; this is the
fastest sanity check before kicking off a real `/feature`.

---

## Next steps

- [The 8-Phase Pipeline](./pipeline.md)
- [The Agents](./agents.md)
- [Customization Guide](./customization.md) — adding agents, restricting permissions
