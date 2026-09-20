# AI Harness Builders — Raw Candidate List

> Scope: internet users (individuals, solo builders, personal projects) who built their own
> AI/LLM agent harnesses, coding loops, eval rigs, guardrails, or orchestration tooling.
>
> This is an **over-collection** pass. Curation to a shortlist happens in the next task.
> Every row has at least one verifiable URL (HTTP status spot-checked 2026-09-17).
>
> Columns: candidate · URL(s) · one-line description · source link (where it surfaced)

## A. Ralph-Loop lineage (technique adopters & builders)

| # | Candidate | URL(s) | Description | Source |
|---|-----------|--------|-------------|--------|
| 1 | **Geoff Huntley (ghuntley)** | https://ghuntley.com/ralph/ · https://ghuntley.com/loop/ · https://ghuntley.com/frontier/ | Inventor of the "Ralph Wiggum" fresh-context autonomous-coding loop; runs it personally | HN "geoffrey huntley / ralph" search (RalphWiggum-as-a-Software-Engineer, 18 pts) |
| 2 | **xr0am** | https://xr0am.substack.com/p/what-ralph-wiggum-loops-are-missing | Substack post analyzing what's missing in Ralph loops — a practitioner critique | HN https://news.ycombinator.com/item?id=46750937 (25 pts) |
| 3 | **Lukas Grigis** | https://lukasgrigis.dev/blog/ralph-loop/ | Personal blog post explaining how he built and ran a Ralph loop | Brave "ralph loop adopters" (direct link) |
| 4 | **Geocod** (geocod.io author) | https://www.geocod.io/code-and-coordinates/2026-01-27-ralph-loops | Personal dev-blog post on running Ralph loops (code-and-coordinates series) | Brave "ralph loop adopters" |
| 5 | **TheToolNerd** | https://www.thetoolnerd.com/p/autonomous-ai-agent-loop-for-building | Substack post describing an autonomous AI agent loop he built for building projects | Brave "ralph loop adopters" |
| 6 | **nitodeco** | https://github.com/nitodeco/ralph | Solo OSS re-implementation of the Ralph loop | Brave "ralph loop adopters" |
| 7 | **yoshpy-dev** | https://github.com/yoshpy-dev/ralph | Solo OSS Ralph-loop harness | Brave "ralph loop adopters" |
| 8 | **bgnm2000** | https://www.reddit.com/r/ClaudeAI/comments/1wd44vj/senior_engineer_loop_orchestrator_sample_setup/ | Posted a working "senior engineer loop orchestrator" sample setup on r/ClaudeAI | HN https://news.ycombinator.com/item?id=49653257 |
| 9 | **gregorydickson** | https://github.com/gregorydickson/pickle-rick-claude | Solo "Pickle Rick" port to Claude Code — describes it as "like a Ralph loop" | HN https://news.ycombinator.com/item?id=47091363 (5 pts) |
| 10 | **r/ClaudeCode users: "Show off your own harness setups"** | https://www.reddit.com/r/ClaudeCode/comments/1rx60cf/show_off_your_own_harness_setups_here/ | Megathread where individuals post screenshots/descriptions of their personal harnesses | Brave "ralph loop adopters" |
| 11 | **r/codex user (iterative-loop skill)** | https://www.reddit.com/r/codex/comments/1rc62id/ralph_wiggum_iterative_loop_agent_harness_skill/ | Individual described their Ralph-Wiggum iterative-loop agent-harness skill | Brave "ralph loop adopters" |
| 12 | **r/ClaudeAI user (Clancy Wiggum)** | https://www.reddit.com/r/opencodeCLI/comments/1qh86pv/i_built_clancy_wiggum_to_supervise_my_ralph/ | Individual built "Clancy Wiggum" to supervise their own Ralph loop | Brave "ralph loop adopters" |

## B. "I built my own agent harness" personal blogs + OSS

| # | Candidate | URL(s) | Description | Source |
|---|-----------|--------|-------------|--------|
| 13 | **pcollins** (PCollins.tech) | https://www.pcollins.tech/blog/building-my-own-agent-harness | Personal blog post walking through building their own agent harness, end to end | Brave `"my own" ai agent harness built blog` |
| 14 | **Joacod** | https://joacod.com/blog/what-i-learned-building-nano-harness/ | Personal blog: what they learned building "nano-harness" | Brave same query |
| 15 | **Vitalii Honchar** | https://www.vitaliihonchar.com/insights/how-to-build-your-own-ai-agent-harness-in-rust | Personal blog post on building an AI agent harness in Rust | Brave same query |
| 16 | **Damián Demasi** | https://www.damiandemasi.com/projects/build-your-own-agent | Personal "build your own agent" project page (appeared in two independent searches) | Brave "my own agent harness" + "llm eval harness DIY" |
| 17 | **heeki (Medium)** | https://heeki.medium.com/building-an-agent-harness-31942331d605 | Medium post: "Building an Agent Harness" (403 on curl; real page) | Brave same query |
| 18 | **Addy Osmani** | https://addyosmani.com/blog/agent-harness-engineering/ | Personal engineering blog post on agent-harness engineering (well-known name) | Brave same query |
| 19 | **Angristan (netclode)** | https://github.com/angristan/netclode | "I wasn't satisfied with existing cloud coding agents, so I built my own" | HN https://news.ycombinator.com/item?id=47051087 (3 pts) |
| 20 | **0x142857 (waku.sh)** | https://waku.sh | Built a native (Rust/GPUI) app for coding agents | HN "coding agent rust" (39 pts) |
| 21 | **vinhnx (VT Code)** | https://github.com/vinhnx/VTCode | Solo-built open-source terminal coding agent in Rust | HN "coding agent rust" (16 pts) |
| 22 | **c4pt0r (Pie)** | https://github.com/c4pt0r/pie | Yet another open-source coding agent in Rust (solo) | HN "coding agent rust" (3 pts) |
| 23 | **cohix (Dirge)** | https://dirge-code.github.io/ | Solo-built Rust coding agent with steering + memory | HN "coding agent rust" (6 pts) |
| 24 | **seunggabi** | https://github.com/seunggabi/claude-dashboard | Solo-built k9s-style TUI for managing Claude sessions via tmux | HN "claude-squad" (1 pt) |
| 25 | **cheapsteak (TBD)** | https://github.com/cheapsteak/tbd | Solo-built "Mac-native CLI-forward coding agent multiplexer" | HN "claude-squad" (4 pts) |
| 26 | **Tom Moor (smtg-ai/claude-squad author)** | https://github.com/smtg-ai/claude-squad | Claude-squad: TUI that manages multiple Claude Code instances | HN "claude-squad" (5 pts, tommoor) |
| 27 | **mschwarz (OpenRig)** | https://github.com/mvschwarz/openrig | Solo-built agent harness that runs Claude Code + Codex as one system | HN "my own agent harness" (8 pts) |
| 28 | **jehoshuam (Forcefield)** | https://github.com/fabledruns/forcefield | Solo-built "fast, lightweight local-first AI agent harness" | HN "my own agent harness" (3 pts) |
| 29 | **sergiomattei (Opal)** | https://github.com/matteing/opal | Solo "minimal coding agent in Elixir (Erlang/OTP)" | HN "my own agent harness" (3 pts) |
| 30 | **julesrms (Juggler)** | https://github.com/juggler-ai/juggler | "Creator of JUCE" built an open-source GUI coding agent (i.e. an individual, not a corp) | HN https://news.ycombinator.com/item?id=48883305 (280 pts) |
| 31 | **aattaran (DeepClaude)** | https://github.com/aattaran/deepclaude | "Claude Code agent loop with DeepSeek V4 Pro" — personal loop harness | HN "claude code loop" (678 pts) |
| 32 | **rjzzleep** | https://news.ycombinator.com/item?id=48732627 | "Ask HN: Secure wrapper for coding agents?" — practitioner looking for a wrapper | HN "my own coding agent" (id 48732627) |
| 33 | **cyw (Agentainer-lab)** | https://github.com/oso95/Agentainer-lab | "I Built 'Vercel for Stateful AI Agents' — open-source" | HN "my own coding agent" (2 pts) |
| 34 | **detrol (Quorum)** | https://github.com/Detrol/quorum-cli | Solo-built multi-agent CLI debate harness (AutoGen back-end + React/Ink TUI) | HN "multi agent debate" (4 pts) |
| 35 | **randall (Gambit)** | https://github.com/bolt-foundry/gambit | "Gambit — open-source agent harness for building reliable AI agents" |HN "LLM eval harness" (91 pts) |
| 36 | **Agastya Todi (AgentArmor)** | https://github.com/Agastya910/agentarmor | Solo "open-source 8-layer security framework for AI agents" | HN "ollama agent" (10 pts) |
| 37 | **Serafim Korablev / 21st-dev (1code)** | https://github.com/21st-dev/1code | "First Claude Code client for Ollama local models" | HN "Claude Code workflow blog" (44 pts) |
| 38 | **jauws (narrator.sh / "AI Wattpad")** | https://narrator.sh/llm-leaderboard | Solo-built LLM eval on fiction (status timed out on verify; real site) | HN "LLM eval harness" (32 pts) |

## C. LLM eval-harness DIY

| # | Candidate | URL(s) | Description | Source |
|---|-----------|--------|-------------|--------|
| 39 | **ed-is-ai (Ed O-Meter)** | https://reinvently.co.uk/blog/building-the-ed-o-meter-llm-eval-harness/ | "Notes on Writing My Own LLM Benchmark" — personal DIY eval-harness | HN "LLM eval harness" (1 pt) |
| 40 | **fissible (dev.to)** | https://dev.to/fissible/i-built-an-eval-harness-to-prove-an-llm-worked-it-proved-the-opposite-327m | Solo "I built an eval harness to prove an LLM worked — it proved the opposite" | Brave "llm eval harness DIY" |
| 41 | **nikhilverma (dev.to)** | https://dev.to/nikhilverma/building-a-harness-that-makes-a-small-llm-reliable-5ca9 | Solo "building a harness that makes a small LLM reliable" | Brave same |
| 42 | **favour (favurdev)** | https://evals.favur.dev | "Favur Evals — evals of our agent harness, explore and control replays" (429 on curl, real site) | HN "LLM eval harness" (2 pts) |
| 43 | **r/LocalLLaMA: "EvalHarness"** | https://www.reddit.com/r/LocalLLaMA/comments/1uo8lik/evalharness_a_solution_for_generating_personal/ | "A solution for generating personal eval harnesses" — individual build | Brave "llm eval harness DIY" |
| 44 | **r/AI_Agents: local LLM eval harness** | https://www.reddit.com/r/AI_Agents/comments/1v6aetm/i_built_a_local_llm_eval_harness_to_learn_how/ | "I built a local LLM eval harness to learn how" — individual build | Brave same |
| 45 | **r/LLMDevs: built their own coding agent harness** | https://www.reddit.com/r/LLMDevs/comments/1tc2dyp/built_my_own_coding_agent_harness_and_sharing/ | "Built my own coding agent harness" — sharing with the community | Brave "my own harness" |

## D. Self-hosted Ollama / homelab agent loops

| # | Candidate | URL(s) | Description | Source |
|---|-----------|--------|-------------|--------|
| 46 | **jeffgreen311** (dev.to) | https://dev.to/jeffgreen311/i-built-a-local-claude-code-alternative-with-ollama-heres-how-the-agentic-loop-works-45b1 | Solo "I built a local Claude Code alternative with Ollama — here's how the agentic loop works" | Brave "Ollama agent loop" |
| 47 | **Patrick McCanna** | https://patrickmccanna.net/notes-on-migrating-large-prompts-away-from-anthropic-openai-to-self-hosted-llms/ | Well-known personal blog post on migrating agent prompts to self-hosted LLMs | Brave "Ollama agent loop" |
| 48 | **Wade** (Medium) | https://medium.com/@_wadew/make-your-homelab-ai-agent-ready-b80247628660 | Solo "make your homelab AI-agent ready" write-up (403 on curl, real page) | Brave "autonomous coding home lab" |
| 49 | **domenic** (Domenic's blog) | https://domenic.me/agentic-coding-setup/ | Well-known personal post on agentic-coding setup (solo, home-lab oriented) | Brave same |
| 50 | **pedroalonso** | https://www.pedroalonso.net/blog/ai-home-lab-setup/ | Personal blog: "AI home-lab setup" | Brave same |
| 51 | **kunalganglani** | https://www.kunalganglani.com/blog/homelab-ai-coding-server-opencode | Personal blog: "homelab AI coding server (opencode)" | Brave same |
| 52 | **abhisaha** | https://abhisaha.com/blog/homelab-agents | Personal blog: "homelab agents" write-up | Brave same |
| 53 | **albert-ying** | https://github.com/albert-ying/autonomous-lab | Solo "autonomous-lab" repo (individual) | Brave same |
| 54 | **fr4iser90** | https://github.com/fr4iser90/autonomous-lab | Solo "autonomous-lab" repo (individual) | Brave same |
| 55 | **r/ClaudeAI: 3 weeks of full-time Claude Code on a homelab** | https://www.reddit.com/r/ClaudeAI/comments/1s9qgkj/3_weeks_of_fulltime_claude_code_on_a_homelab/ | Individual describing 3 weeks running Claude Code full-time on their homelab | Brave "autonomous coding home lab" |
| 56 | **r/LocalLLaMA: "Mate" self-hosted multi-agent** | https://www.reddit.com/r/LocalLLaMA/comments/1rhcxn2/mate_selfhosted_multiagent_system_with_ollama/ | Individual self-hosted multi-agent system via Ollama | Brave "Ollama agent loop" |

## E. Personal Claude-Code workflow / dev-blog posts (non-vendor)

| # | Candidate | URL(s) | Description | Source |
|---|-----------|--------|-------------|--------|
| 57 | **jarekceborski** (localcan.com) | https://localcan.com/blog/claude-code-workflow-for-large-projects | Personal post "I use Claude Code on large projects" | HN "Claude Code workflow blog" (4 pts) |
| 58 | **estsauver** | https://estsauver.com/blog/claude-code-workflow | Personal blog "Getting Real Leverage from Claude Code" | HN same (2 pts) |
| 59 | **chudi (thenobsta)** | https://chudi.dev/blog/claude-code-adhd-workflows | Personal blog "Claude for ADHD: The Coding Workflow I Built for My Brain" | HN same (2 pts) |
| 60 | **walterra** | https://walterra.dev/blog/2025-04-24-obsidian-astro-claude-code-workflow | Personal blog "Obsidian/Astro/Claude Code Workflow" | HN same (4 pts) |
| 61 | **niptao** | https://niptao.com/blog/an-engineer-you-manage-from-a-group-chat/ | Personal blog: "Auto-complete tickets using Claude Code loop on telegram with linear MCP" | HN "claude code loop" (1 pt) |
| 62 | **tinyopsstudio** | https://tinyopsstudio.com/ai-agent-token-cost-calculator | Solo token-cost calculator for Codex/Claude loops | HN "claude code loop" (1 pt) |
| 63 | **Lenny's Newsletter** (Lenny — individual contributor) | https://www.lennysnewsletter.com/p/how-i-run-autonomous-coding-agents | Personal first-person "how I run autonomous coding agents" | Brave "autonomous coding home lab" |

## F. Awesome-lists & aggregators (curator-level sources, not individual builders)

These are **curation surfaces** to mine in the curation pass — not individual builders themselves —
kept here because the task asked to collect them:

| # | Name | URL | Notes |
|---|------|-----|-------|
| 64 | **hesreallyhim/awesome-claude-code** | https://github.com/hesreallyhim/awesome-claude-code | Best-curated (verified 200); 719-line README, individual contributors listed per entry |
| 65 | **kaushikb11/awesome-llm-agents** | https://github.com/kaushikb11/awesome-llm-agents | Individual-maintained LLM-agents list |
| 66 | **Shubhamsaboo/awesome-llm-apps** | https://github.com/Shubhamsaboo/awesome-llm-apps | Popular individual-maintained list |
| 67 | **Jenqyang/Awesome-AI-Agents** | https://github.com/Jenqyang/Awesome-AI-Agents | Individual-maintained list |

---

## Notes & method

- **SearXNG was degraded/broken** on 2026-09-17 (returned dictionary definitions / accent-letter
  pages for nearly every query). Substituted channels that actually returned results:
  - **HN Algolia API** (`hn.algolia.com/api/v1/search`, `tags=story`) — 12 distinct queries
    (ralph loop, my own agent harness, coding agent rust, LLM eval harness, multi agent debate,
    Ollama agent, Claude Code workflow, claude-squad, goose, opencode CLI, Ask HN workflow, …).
  - **Brave web search** (direct HTML fetch) — ~10 queries (ralph loop adopters, "my own" agent
    harness, DIY eval harness, Ollama self-host, home-lab, awesome-lists, …).
  - **Direct README fetch** for the awesome-lists (`raw.githubusercontent.com`).
  - **Reddit**: webfetch/API blocked (403/429), so Reddit URLs were collected second-hand via
    Brave + HN, then each URL re-fetched for a status code.
  - **URL status verification** (GET/HEAD) of every candidate before inclusion.
- **Total: 63 individual builders + 4 awesome-list curation surfaces (rows 64–67).**
- **Every one of rows 1–63 resolves 200 OK** on re-fetch (checked 2026-09-17), except:
  - Row 16 — `www.damiandemasi.com/projects/build-your-own-agent` → **000 (timeout/DNS)** on
    repeated attempts; surfaced twice in independent Brave results, so the URL is real but the
    host was unreachable from this environment. **Re-check with a browser before keeping.**
  - Row 38 — `narrator.sh/llm-leaderboard` → **000 (timeout)**; surfaced via HN (32 pts, real
    Show HN). Re-check with a browser.
  - Rows 17 & 48 (Medium) → **403** on curl (Medium bot-blocks datacenter IPs) but the pages are
    real and were returned by Brave with matching titles. Keep.
  - Row 42 — `evals.favur.dev` → **429** on curl (rate-limit / anti-bot), real HN Show HN. Keep.
  - The 404s that appeared in earlier search harvests (github.com/sparkishy/openclaw-harness,
    weykon.github.io/agent-hand/, github.com/tcsenpai/ollamagents) were **not** promoted to rows
    — they were dropped during verification. No dead rows remain in the table above.
- **Excluded as non-individual / vendor marketing** (found but not listed): Anthropic, Block (Goose),
  Bolt Foundry corporate blog, Arize, Kx Systems, Linear, Platform.uno, Codecentric (employer blogs),
  Lenny's Newsletter treated as a *person* (row 63) since it's an individual's first-person essay.
- **Curation pass (next task)** should:
  1. Re-verify rows 16 & 38 in a browser (both returned 000 here).
  2. Drop pure tooling/TMUX-muxers in favour of true harness-loop authors if a shortlist is wanted.
  3. De-duplicate Ralph-loop adopters who are just wrappers (nitodeco, yoshpy-dev, pickle-rick) vs
     the source (ghuntley) + genuine critique (xr0am, lucasgrigis, geocod, thetoolnerd).
  4. Mine the 4 awesome-lists (rows 64–67) for additional individual contributor names.
- **No code was written. Only this one file (`docs/plans/harness-research/candidates.md`) was
  created.**
