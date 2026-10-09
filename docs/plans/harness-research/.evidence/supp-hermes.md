# Hermes Financial

> **An open-source framework for building AI investment research systems.**

Hermes is the reusable Python framework a developer reaches for when building
professional AI investment-research applications. It ships pre-built
financial data tools, document ingestion pipelines, output generation
(Excel models, Word/PDF reports, charts), composable agents, and the
infrastructure you need to make those agents reliable, observable, and
extensible.

Hermes does **not** answer *"what should I buy?"* — that question belongs to
your application, your strategy, and your judgment. What it answers:

> *"How do I give AI agents the tools, data, workflows, provenance, and
> output infrastructure needed to perform serious investment research?"*

A downstream application can use Hermes as its research substrate without
modifying Hermes itself — registering custom agents, workflows, tools,
storage adapters, and event sinks.

---

## Quick Start

```bash
pip install hermes-financial
```

```python
import asyncio
from hermes import Hermes, configure

configure(
    llm_provider="anthropic",
    sec_user_agent="MyApp/1.0 (me@company.com)",  # required by SEC
)

h = Hermes()
result = h.invoke("Research Apple's latest quarter")
print(result["response"])
```

The default `invoke()` returns a dict; for richer output use
`await h.run_structured(...)` to receive a `ResearchResult` (see
[Structured outputs](#structured-outputs)).

---

## What's Included

### Data Tools (free, no proprietary keys required for the basics)

| Module | What you get | Source |
|---|---|---|
| **SEC EDGAR** | Company facts (XBRL), filing search, submissions, filing URLs, structured financial tables, labeled text sections (MD&A, Risk Factors, …), insider transactions, institutional holdings | [edgartools](https://github.com/dgunning/edgartools) + `efts.sec.gov` |
| **FRED** | Economic time series, series search, metadata | `api.stlouisfed.org` (free key) |
| **Market Data** | Quotes, historical OHLCV, batch quotes | Yahoo Finance |
| **News** | Company news, general financial news | RSS feeds |
| **Excel** | Create, write, read, format, formulas, charts, save `.xlsx` | `openpyxl` |
| **Documents** | Create, headings, paragraphs, tables, images, save `.docx`, export PDF | `python-docx` |
| **Charts** | Line, bar, waterfall, scatter, heatmap → PNG | `matplotlib` |

### Specialist Agents (7 built-in)

| Agent | Type | Role |
|---|---|---|
| `SecFilingsAgent` | FunctionAgent | SEC filing retrieval & analysis |
| `MacroAgent` | FunctionAgent | FRED macroeconomic data |
| `MarketDataAgent` | FunctionAgent | Quotes and historical prices |
| `NewsAgent` | FunctionAgent | News/sentiment |
| `ModelingAgent` | ReActAgent | Excel financial model construction |
| `ReportAgent` | ReActAgent | Word/PDF report generation |
| `ResearchOrchestrator` | FunctionAgent | Multi-agent coordination |

### Architecture (since 0.2.0)

```
hermes/
├── agents/        # Specialist agents (built-in + custom HermesAgent subclasses)
├── workflows/     # ResearchWorkflow / EquityResearchWorkflow + HermesWorkflow protocol
├── tools/         # Data + output tools, each exposes create_tools()
├── schemas/       # Source, Evidence, ResearchArtifact, ResearchRun, ResearchResult,
│                  # CompanyProfile, FinancialAnalysis, ValuationAnalysis, CompanyResearch
├── provenance/    # ProvenanceCollector + current_collector() contextvar
├── storage/       # ResearchStore / ArtifactStore protocols + in-memory + local impls
├── ingestion/     # Optional: filing / transcript parsers + ChromaDB RAG (rag extra)
├── infra/         # Cache, rate limiter, retry, streaming events
├── config.py      # Pydantic HermesConfig (HERMES_ env prefix)
├── llm_providers.py   # Provider registry (10 built-in + openai_compatible + openrouter)
├── registry.py    # Tool/agent/workflow registry (per Hermes instance)
└── core.py        # The Hermes facade
```

---

## Configuration

All settings are accepted as kwargs to `configure()` or via environment
variables with the `HERMES_` prefix:

```python
from hermes import Hermes, configure

configure(
    llm_provider="anthropic",                  # or openai, google, ollama, ...
    llm_model="claude-sonnet-4-6",
    sec_user_agent="MyApp/1.0 (me@company.com)",
    fred_api_key="...",
    verbose=True,
)
```

### OpenAI-compatible endpoints (OpenRouter, vLLM, self-hosted)

Hermes ships a generic `openai_compatible` provider and a preconfigured
`openrouter` convenience. Set the base URL and key via config:

```python
configure(
    llm_provider="openai_compatible",
    llm_model="my-model",
    llm_api_base="https://internal.example.com/v1",
    llm_api_key="...",
    llm_context_window=32000,                # optional
    llm_is_function_calling_model=True,      # optional
)
```

Or for OpenRouter:

```python
configure(
    llm_provider="openrouter",
    llm_model="anthropic/claude-sonnet-4-5",
    llm_api_key="...",                        # OpenRouter API key
)
```

See [`examples/openrouter.py`](examples/openrouter.py).

Add your own providers at runtime:

```python
from hermes.llm_providers import ProviderSpec, register_provider

register_provider(
    ProviderSpec(
        name="my_internal",
        import_module="llama_index.llms.openai_like",
        class_name="OpenAILike",
        extra_kwargs={"api_base": "https://internal.example/v1"},
    )
)
```

---

## Extending Hermes

### Custom agent

```python
from hermes.agents.base import HermesAgent
from hermes import Hermes

class SupplyChainAgent(HermesAgent):
    name = "supply_chain"
    description = "Analyzes supplier concentration and logistics risks."
    system_prompt = "You are a supply chain analyst..."
    agent_type = "function"

    def get_tools(self):
        return [...]

h = Hermes()
h.register_agent("supply_chain", SupplyChainAgent)
```

See [`examples/custom_agent.py`](examples/custom_agent.py).

### Custom workflow

A workflow composes a root agent with a set of specialists and decides
how they hand off. Built-in `EquityResearchWorkflow` is the default;
you can compose your own with `ResearchWorkflow` (or any class
implementing the `HermesWorkflow` protocol):

```python
from hermes import Hermes
from hermes.workflows import ResearchWorkflow

workflow = ResearchWorkflow(
    root_agent="orchestrator",
    agents=[
        "sec_filings",
        "market_data",
        SupplyChainAgent(),
    ],
)

h = Hermes()
result = await h.run("Research NVDA", workflow=workflow)
```

See [`examples/custom_workflow.py`](examples/custom_workflow.py).

### Per-agent LLM overrides (cost / latency / quality)

```python
from hermes.workflows import ResearchWorkflow

workflow = ResearchWorkflow(
    agent_llms={
        "sec_filings": cheap_llm,    # fast/cheap model for data extraction
        "modeling": strong_llm,      # strongest model for the model agent
        "report": writing_llm,       # different model for prose
    },
)
```

### Structured outputs

`run_structured()` returns a `ResearchResult` with a full `ResearchRun`
record: participating agents, provider/model, status, timestamps,
sources, and artifacts. Failures don't raise — they return
`ResearchResult` with `run.status = failed` and `run.error` set.

```python
result = await h.run_structured("Research AMBA")
print(result.response)
print(result.run.agents)
print(result.run.sources)
```

See [`examples/structured_results.py`](examples/structured_results.py).

### Event sinks

`Hermes(event_sinks=[...])` fans every event (workflow started,
agent output, tool call, artifact created, …) out to every callable
sink. Async sinks are scheduled; sync sinks block. Sink exceptions are
logged but don't abort the run.

```python
def my_sink(event):
    print(f"[{event.run_id}] {event.type}: {event.text or event.metadata}")

h = Hermes(event_sinks=[my_sink])
```

### Storage

By default runs live in memory. Replace with a local-disk or remote
implementation:

```python
from hermes import Hermes
from hermes.storage import LocalResearchStore

h = Hermes(research_store=LocalResearchStore("~/.hermes/runs"))
```

Implement the `ResearchStore` / `ArtifactStore` protocols to plug in
your own backend (D1, R2, S3, Postgres, …).

See [`examples/storage_adapter.py`](examples/storage_adapter.py).

### Custom tools (registry-based)

```python
from llama_index.core.tools import FunctionTool
from hermes import Hermes

tool = FunctionTool.from_defaults(fn=my_lookup, name="my_lookup")
h = Hermes()
h.register_tool("my_lookup", tool, tags=["custom"])
```

Custom agents can pull registered tools by name via the
`HermesAgent.tool_names` class attribute — no need to import the
module that created them.

---

## Using Tools Directly

You don't need agents to use the tools. Every tool module exposes
async functions that work standalone:

```python
from hermes.tools.sec_edgar import (
    get_company_facts,
    get_filing_urls,
    get_filing_financial_tables,
    get_filing_text,
)
from hermes.tools.fred import get_series
from hermes.tools.excel import excel_create_workbook, excel_write_cells, excel_save

facts = await get_company_facts("AAPL")
filings = await get_filing_urls("AAPL", filing_types="10-K,10-Q", limit=10)
tables = await get_filing_financial_tables("AAPL", filings[0]["accessionNumber"])
text = await get_filing_text(filings[0]["url"])
gdp = await get_series("GDP", start_date="2020-01-01")
```

---

## Optional Extras

| Extra | What it adds | Install |
|---|---|---|
| `rag` | ChromaDB-backed semantic search over filings/transcripts/user documents | `pip install hermes-financial[rag]` |
| `google`, `mistral`, `groq`, `ollama`, `huggingface`, `cohere`, `deepseek`, `xai` | Corresponding LLM provider | `pip install hermes-financial[google]` (etc.) |
| `llamaparse` | LlamaParse for parsing PDFs | `pip install hermes-financial[llamaparse]` |
| `web` | Web page readers | `pip install hermes-financial[web]` |
| `pandas` | Pandas-shaped returns | `pip install hermes-financial[pandas]` |
| `dev` | Test/lint/type tooling | `pip install hermes-financial[dev]` |
| `all` | Everything above | `pip install hermes-financial[all]` |

---

## Documentation

- [`docs/providers.md`](docs/providers.md) — provider configuration, custom providers, OpenRouter
- [`docs/workflows.md`](docs/workflows.md) — composing custom workflows
- [`docs/agents.md`](docs/agents.md) — custom agents and `tool_names`
- [`docs/storage.md`](docs/storage.md) — custom ResearchStore / ArtifactStore
- [`docs/events.md`](docs/events.md) — event vocabulary and sinks
- [`docs/provenance.md`](docs/provenance.md) — source/evidence provenance
- [`docs/schemas.md`](docs/schemas.md) — structured outputs

---

## Development

```bash
git clone https://github.com/hermes-financial/hermes-financial.git
cd hermes-financial
uv sync --all-extras

uv run pytest          # 340+ tests
uv run ruff check hermes/ tests/
uv run mypy hermes/
```

---

## Requirements

- Python >= 3.10
- An LLM API key (Anthropic, OpenAI, OpenRouter, …) for agent functionality
- SEC EDGAR requires a `User-Agent` string identifying your application
- FRED requires a free API key from [fred.stlouisfed.org](https://fred.stlouisfed.org/docs/api/api_key.html)

## License

MIT — see [LICENSE](LICENSE).

---

Hermes is the research framework. Downstream applications are the
investors.
