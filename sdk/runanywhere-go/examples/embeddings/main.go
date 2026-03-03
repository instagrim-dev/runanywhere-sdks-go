// embeddings demonstrates the v2 Embeddings API: call Health to check capabilities,
// then embed a single string or a slice of strings. The server must be started with
// --embeddings-model for this to succeed; otherwise the endpoint returns 501.
//
// Usage:
//
//	go run . [-url URL] [-batch "a" "b" "c"]
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"

	"github.com/runanywhere/runanywhere-sdks-go/sdk/runanywhere-go"
)

func main() {
	baseURL := flag.String("url", "http://127.0.0.1:8080", "Server base URL")
	batch := flag.String("batch", "", "Comma-separated strings to embed as batch (default: single string 'hello')")
	flag.Parse()

	client := runanywhere.NewClient(*baseURL)
	ctx := context.Background()

	health, err := client.Health(ctx)
	if err != nil {
		log.Fatalf("Health: %v", err)
	}
	fmt.Printf("Health: status=%s model=%s embeddings_available=%v\n",
		health.Status, health.Model, health.EmbeddingsAvailable)
	if !health.EmbeddingsAvailable {
		log.Fatal("Embeddings not configured: start server with --embeddings-model")
	}

	model := health.Model
	if model == "" {
		model = "default"
	}

	if *batch != "" {
		// Batch: input as slice of strings (e.g. "one,two,three")
		inputs := splitBatch(*batch)
		resp, err := client.Embeddings(ctx, &runanywhere.EmbeddingsRequest{
			Model: model,
			Input: inputs,
		})
		if err != nil {
			if se, ok := err.(*runanywhere.ServerError); ok && se.StatusCode == 501 {
				log.Fatalf("Embeddings not configured: %s (start server with --embeddings-model)", se.Message)
			}
			log.Fatalf("Embeddings: %v", err)
		}
		fmt.Printf("Model: %s\n", resp.Model)
		for i, d := range resp.Data {
			fmt.Printf("  [%d] dimension=%d\n", i, len(d.Embedding))
		}
		return
	}

	// Single string
	resp, err := client.Embeddings(ctx, &runanywhere.EmbeddingsRequest{
		Model: model,
		Input: "hello",
	})
	if err != nil {
		if se, ok := err.(*runanywhere.ServerError); ok && se.StatusCode == 501 {
			log.Fatalf("Embeddings not configured: %s (start server with --embeddings-model)", se.Message)
		}
		log.Fatalf("Embeddings: %v", err)
	}
	fmt.Printf("Model: %s\n", resp.Model)
	if len(resp.Data) > 0 {
		fmt.Printf("Embedding dimension: %d\n", len(resp.Data[0].Embedding))
	}
}

func splitBatch(s string) []string {
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return []string{"hello"}
	}
	return out
}
