# Quickstart — OpenCode

Get from zero to a running feature pipeline using **OpenCode** and MAPLE.

---

## Prerequisites

| Tool | Install |
|---|---|
| [OpenCode](https://opencode.ai) | `npm install -g opencode-ai` |
| [GitHub CLI](https://cli.github.com) | `brew install gh` |
| [Git](https://git-scm.com) | pre-installed on macOS/Linux |
| [Go 1.22+](https://go.dev) | `brew install go` *(only to build from source)* |
| [Node.js](https://nodejs.org) | `brew install node` *(Playwright E2E tests + npx skills)* |
| [Docker](https://docker.com) | [docker.com/get-started](https://docker.com/get-started) |

---

## 1. Install `maple`

**Pre-built binary (recommended):**

```bash
curl -fsSL https://raw.githubusercontent.com/kinncj/maple/main/scripts/install.sh | bash
```

Installs to `~/.tools/maple/bin/`. Add to your shell profile:

```bash
export PATH="$HOME/.tools/maple/bin:$PATH"
```

**From source:**

```bash
git clone https://github.com/kinncj/maple.git
cd maple
make build-tui           # produces ./maple
export PATH="$PWD:$PATH"
```

Verify: `maple --version`

---

## 2. Configure OpenCode providers

MAPLE ships a default `opencode.json`. The recommended way to wire up a provider is
to pass `--provider` when scaffolding the project:

```bash
cd your-project-directory
maple init --provider anthropic       # public Anthropic API
maple init --provider openai          # OpenAI
maple init --provider github-copilot  # GitHub Copilot (aliases: copilot)
maple init --provider amazon-bedrock  # AWS Bedrock (aliases: bedrock, aws)
```

This writes the provider block into `opencode.json`, sets a sensible default model,
and stamps every agent's `model:` frontmatter with a tier-appropriate model ID
(Opus tier for `orchestrator`/`architect`, Sonnet tier for implementation agents,
Haiku tier for `docs`/`rubber-duck`).

Pin every agent to one specific model:

```bash
maple init --provider openai --model openai/gpt-5
```

The choice is persisted in `.maple/provider.json` and replayed by every later
`maple update`, so you don't need to pass the flag again.

### Provider-specific credentials

| Provider | Credential |
|---|---|
| `anthropic` | `export ANTHROPIC_API_KEY=sk-ant-...` |
| `openai` | `export OPENAI_API_KEY=sk-...` |
| `github-copilot` | `gh auth login --scopes copilot` |
| `amazon-bedrock` | `export AWS_PROFILE=...` (or `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY`) + `AWS_REGION`. See [Quickstart — AWS Bedrock](./quickstart-bedrock.md). |

---

## 3. Scaffold your project

```bash
cd your-project-directory
maple init
```

`maple init` copies `.opencode/` agents along with skills, hooks, Makefile stubs, and docs structure into the current directory.

---

## 4. Customize the Makefile

The Makefile ships with stubs. Open `Makefile` and replace the recipe bodies with your stack's commands before running any feature pipeline.

---

## 5. Bootstrap GitHub

```bash
gh auth login
maple labels    # create MAPLE phase labels on the repo
maple project   # create a GitHub Project v2 board
```

---

## 6. Write your first story

Press `n` in the `maple` dashboard to open the Gherkin requirements wizard, or run `maple req` directly. The wizard produces a story file at `docs/stories/{slug}/Story.md` with embedded Gherkin and links it to a GitHub Issue.

---

## 7. Run a feature

Open the project in **OpenCode**:

```bash
opencode .
```

Then run:

```
/feature "short description of what you want to build"
```

The orchestrator follows the same 8-phase pipeline as in Claude Code. Agent routing in OpenCode uses the `permission.task` list in `.opencode/agents/orchestrator.md`.

---

## Agent model routing

Every agent in `.opencode/agents/{name}.md` has a `model:` field in its frontmatter,
written by `maple init --provider <name>`. Tiers:

| Tier | Agents | Why |
|---|---|---|
| deep | `orchestrator`, `architect` | Pipeline control + ADRs need the strongest reasoning. |
| fast | most implementation agents | Cost/latency sweet spot for code. |
| small | `docs`, `rubber-duck`, `humanizer`, `spec-kit` | Light prose / review tasks. |

Deviate from the defaults by editing an agent's `model:` to a value that does **not**
start with one of MAPLE's known provider prefixes (`anthropic/`, `openai/`,
`github-copilot/`, `amazon-bedrock/`). Such hand-set values are preserved across
`maple update`. To pin everything to one model, use `--model <id>` on init/update.

---

## Commands reference

| Command | What it does |
|---|---|
| `/feature "description"` | Full 8-phase pipeline |
| `/bugfix "description"` | Reproduce → fix → validate → CHANGELOG |
| `/validate` | Run full test suite |
| `/tdd "requirement"` | Single RED → GREEN → REFACTOR cycle |

---

## Next steps

- [The 8-Phase Pipeline](./pipeline.md)
- [The Agents](./agents.md)
- [Customization Guide](./customization.md)
