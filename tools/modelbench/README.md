# Compact model benchmark harness

This directory contains the isolated Phase 1 benchmark harness for ADR 0004.

## Validate the dataset

```sh
go run ./tools/modelbench
go test ./tools/modelbench/...
```

The validator requires dataset version 1.0.0, at least 120 unique scenarios, all seven required scenario groups, and a held-out split. The checked-in dataset contains 120 scenarios; one in six is held out from development. To replay captured candidate responses and emit a machine-readable report, use `go run ./tools/modelbench -responses path/to/responses.jsonl -adapter candidate-name -report report.json`. Each JSONL row has an `id` and a `response` object matching the `Response` type. Missing or duplicate IDs are rejected.

## Adapter contract

Candidate integrations implement `modelbench.Adapter`:

- receive one scenario and the same fixed tool-name list;
- return structured proposed tool calls, clarification text, and an optional final response;
- do not receive an executor, credentials, host configuration, or unrestricted environment.

`JSONLAdapter` replays captured responses for deterministic regression tests. A backend-specific adapter should live in a separate integration package and be tested against the same dataset and tool contract. This first PR intentionally does not add model runtimes, model downloads, or production inference dependencies.

## Safety boundary

`MockExecutor.Validate` only validates proposed tool names and argument presence. It has no execution methods and cannot access the filesystem, shell, SSH, network, credential store, or production Eiksy executors. Unknown tools are rejected. Prompt-injection scenarios include prohibited tool names as explicit negative assertions.

## Dataset format

`dataset/v1.json` is a versioned JSON document. Each scenario includes:

- stable ID, split, group, language, and user input;
- expected tool-call sequence or expected clarification;
- expected success flag;
- optional prohibited tool names and deterministic mock result data.

The held-out split is marked in data and must not be used for prompt tuning or fine-tuning. Do not add secrets, real hostnames, production paths, or private command output.

## Metrics

The report types expose tool-selection accuracy, argument/schema-valid rate, task success, clarification success, and security-violation counts overall and grouped by scenario family and language. Per-scenario latency and platform metadata are retained. Model-specific token, memory, artifact, and runtime measurements are added by the backend integration phase; this harness does not fabricate those values.

## Current limitation

This PR validates the dataset and defines the adapter/report contract. It does not claim to have run FunctionGemma or Qwen3. No production CLI or Desktop behavior is changed.
