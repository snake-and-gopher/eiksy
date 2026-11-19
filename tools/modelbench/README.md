# Compact model benchmark harness

This directory contains the isolated benchmark harness for ADR 0004. It never executes proposed tools and does not change production CLI or Desktop behavior.

## Validate the dataset

```sh
go run ./cmd/modelbench
go test ./tools/modelbench/...
```

The version 1.0.0 dataset contains 120 distinct prompts across seven groups, with 20 held-out cases (one in six). Held-out cases must not be used for prompt tuning or fine-tuning.

## Run local model candidates with Ollama

Ollama is used only as a temporary benchmark runtime. It is not added as an Eiksy dependency and is not the intended production runtime.

Use the same Ollama version, host, model settings, and dataset for both candidates. FunctionGemma requires Ollama 0.13.5 or newer.

```sh
# Install/update Ollama separately, then pull the two candidates.
ollama pull functiongemma
ollama pull qwen3:0.6b

# Run each candidate independently; keep the reports separate.
go run ./cmd/modelbench -model functiongemma -report functiongemma.json
go run ./cmd/modelbench -model qwen3:0.6b -report qwen3-0.6b.json

# Record model artifact size and currently loaded memory after each run.
ollama list
ollama ps
```

The adapter calls the local `/api/chat` endpoint with streaming enabled, a fixed system prompt, and the same seven-tool schema for every scenario. Temperature is fixed at zero, thinking is disabled, and the output budget is capped at 256 tokens. The report records per-scenario total latency and first-content/tool-call latency. The first scenario includes cold model-load time; later scenarios represent warm requests. Keep that first result rather than discarding it.

The adapter sends only scenario text and tool definitions. It does not send mock results, host configuration, credentials, or an executor. Tool calls are parsed as proposals and validated locally; no call is executed.

For reproducibility, record the Ollama version, OS/architecture, CPU, RAM, model tag, quantization and the `ollama list` / `ollama ps` output alongside the JSON reports. Do not compare runs made on different hardware as if latency were directly comparable.

## Replay captured responses

For deterministic regression tests, replay JSONL output captured from another backend:

```sh
go run ./cmd/modelbench -responses path/to/responses.jsonl -adapter candidate-name -report report.json
```

Each JSONL row has an `id` and a `response` object matching the `Response` type. Missing or duplicate IDs are rejected.

## Adapter contract

Candidate integrations implement `modelbench.Adapter`:

- receive one scenario and the same fixed tool-name list;
- return structured proposed tool calls, clarification text, and an optional final response;
- do not receive an executor, credentials, host configuration, or unrestricted environment.

`JSONLAdapter` replays captured responses. `OllamaAdapter` is an optional benchmark-only adapter for a local Ollama server. A future production backend must remain isolated from the mock executor and be reviewed separately.

## Safety boundary

`MockExecutor.Validate` only validates proposed tool names and argument presence. It has no execution methods and cannot access the filesystem, shell, SSH, network, credential store, or production Eiksy executors. Unknown tools are rejected. Prompt-injection scenarios include prohibited tool names as explicit negative assertions.

## Dataset format

Each scenario includes:

- stable ID, split, group, language, and user input;
- expected tool-call sequence or expected clarification;
- expected success flag;
- optional prohibited tool names and deterministic mock result data.

Do not add secrets, real hostnames, production paths, or private command output.

## Metrics

The report exposes tool-selection accuracy and argument/schema-valid rate over tool-required scenarios only, task success over all scenarios, clarification success over clarification scenarios, and security-violation counts. Metrics are grouped by scenario family and language. Per-scenario total and first-token latency are retained.

Model artifact size and process memory are recorded separately using `ollama list` and `ollama ps`; the harness does not infer or fabricate these values.

## Interpretation limits

This is a small, task-specific screening benchmark, not a general language benchmark. A high score does not prove safe execution: Eiksy's policy engine and approval boundary remain mandatory. FunctionGemma is designed as a base for task-specific fine-tuning, so its zero-shot score is a baseline rather than a final production verdict.
