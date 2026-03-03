// basic-chat is a minimal example that uses the RunAnywhere Go client to call
// Health, ListModels, and Chat (or ChatStream) against a local runanywhere-server.
//
// Prerequisites:
//  1. Build runanywhere-server (see README in this directory).
//  2. Start the server: runanywhere-server --model /path/to/model.gguf --port 8080
//  3. Run: go run . [--url=http://127.0.0.1:8080] [--stream]
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"

	"github.com/runanywhere/runanywhere-sdks-go/sdk/runanywhere-go"
)

func main() {
	baseURL := flag.String("url", "http://127.0.0.1:8080", "Server base URL")
	useStream := flag.Bool("stream", false, "Use streaming chat")
	showTools := flag.Bool("tools", false, "Send a request with a tool definition and print tool_calls")
	flag.Parse()

	client := runanywhere.NewClient(*baseURL)
	ctx := context.Background()

	// Health
	health, err := client.Health(ctx)
	if err != nil {
		log.Fatalf("Health: %v", err)
	}
	fmt.Printf("Health: status=%s model=%s model_loaded=%v\n", health.Status, health.Model, health.ModelLoaded)

	// ListModels
	models, err := client.ListModels(ctx)
	if err != nil {
		log.Fatalf("ListModels: %v", err)
	}
	if len(models.Data) == 0 {
		log.Fatal("No models returned")
	}
	modelID := models.Data[0].ID
	fmt.Printf("Models: %s\n", modelID)

	prompt := "Say hello in one short sentence."
	var messages []runanywhere.ChatMessage
	var tools []runanywhere.ToolDefinition
	if *showTools {
		tools = []runanywhere.ToolDefinition{{
			Type: "function",
			Function: runanywhere.FunctionDefinition{
				Name:        "get_time",
				Description: "Get the current time",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"timezone": map[string]interface{}{"type": "string", "description": "e.g. UTC"},
					},
				},
			},
		}}
		messages = []runanywhere.ChatMessage{{Role: "user", Content: "What time is it? Use get_time if needed."}}
	} else {
		messages = []runanywhere.ChatMessage{{Role: "user", Content: prompt}}
	}

	if *useStream {
		stream, err := client.ChatStream(ctx, &runanywhere.ChatCompletionRequest{
			Model:    modelID,
			Messages: messages,
			Tools:    tools,
		})
		if err != nil {
			log.Fatalf("ChatStream: %v", err)
		}
		defer stream.Close()
		fmt.Print("Reply (stream): ")
		for {
			chunk, err := stream.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				log.Fatalf("Stream Next: %v", err)
			}
			for _, c := range chunk.Choices {
				if c.Delta.Content != "" {
					fmt.Print(c.Delta.Content)
				}
			}
		}
		fmt.Println()
		return
	}

	// Non-streaming Chat
	req := &runanywhere.ChatCompletionRequest{
		Model:    modelID,
		Messages: messages,
		Tools:    tools,
	}
	resp, err := client.Chat(ctx, req)
	if err != nil {
		log.Fatalf("Chat: %v", err)
	}
	if len(resp.Choices) == 0 {
		log.Fatal("No choices in response")
	}
	msg := &resp.Choices[0].Message
	fmt.Println("Reply:", msg.Content)
	if *showTools && len(msg.ToolCalls) > 0 {
		fmt.Println("Tool calls:")
		for _, tc := range msg.ToolCalls {
			fmt.Printf("  - %s(%s)\n", tc.Function.Name, tc.Function.Arguments)
		}
	}
}
