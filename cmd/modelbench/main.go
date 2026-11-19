package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"eiksy/tools/modelbench"
)

func main() {
	datasetPath := flag.String("dataset", "tools/modelbench/dataset/v1.json", "versioned benchmark dataset")
	responsesPath := flag.String("responses", "", "JSONL file with captured adapter responses")
	reportPath := flag.String("report", "benchmark-report.json", "output JSON report")
	adapterName := flag.String("adapter", "jsonl-replay", "adapter name recorded in report")
	modelName := flag.String("model", "", "run a model through the local Ollama API instead of replay")
	ollamaURL := flag.String("ollama-url", "http://localhost:11434", "Ollama API base URL")
	flag.Parse()

	d, err := modelbench.LoadDataset(*datasetPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "benchmark dataset:", err)
		os.Exit(1)
	}
	fmt.Printf("Dataset %s: %d scenarios validated.\n", d.Version, len(d.Scenarios))

	var adapter modelbench.Adapter
	if *modelName != "" {
		adapter = modelbench.NewOllamaAdapter(*ollamaURL, *modelName)
	} else {
		if *responsesPath == "" {
			fmt.Println("No model was run. Use -model with a local Ollama server, or -responses to replay captured outputs.")
			return
		}
		input, err := os.ReadFile(*responsesPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "benchmark responses:", err)
			os.Exit(1)
		}
		adapter, err = modelbench.NewJSONLAdapter(*adapterName, input)
		if err != nil {
			fmt.Fprintln(os.Stderr, "benchmark adapter:", err)
			os.Exit(1)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	report := modelbench.Run(ctx, d, adapter)
	if err := modelbench.WriteReport(*reportPath, report); err != nil {
		fmt.Fprintln(os.Stderr, "benchmark report:", err)
		os.Exit(1)
	}
	fmt.Printf("Report written to %s (%d scenarios, adapter %s).\n", *reportPath, len(report.Results), adapter.Name())
}
