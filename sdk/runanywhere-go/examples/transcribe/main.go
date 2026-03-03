// transcribe demonstrates the v2 Transcribe API: send an audio file to the server
// and print the transcript. The server must be started with --stt-model for this to succeed;
// otherwise it returns 501.
//
// Usage:
//   go run . -url http://127.0.0.1:8080 -audio audio.wav
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/runanywhere/runanywhere-sdks-go/sdk/runanywhere-go"
)

func main() {
	baseURL := flag.String("url", "http://127.0.0.1:8080", "Server base URL")
	audioPath := flag.String("audio", "", "Path to audio file (e.g. .wav)")
	language := flag.String("lang", "", "Optional language code (e.g. en)")
	flag.Parse()

	if *audioPath == "" {
		fmt.Fprintln(os.Stderr, "Usage: transcribe -audio <path> [-url URL] [-lang en]")
		fmt.Fprintln(os.Stderr, "Server must be started with --stt-model for transcriptions; otherwise 501.")
		os.Exit(1)
	}

	f, err := os.Open(*audioPath)
	if err != nil {
		log.Fatalf("Open audio: %v", err)
	}
	defer f.Close()

	client := runanywhere.NewClient(*baseURL)
	ctx := context.Background()

	opts := (*runanywhere.TranscribeOptions)(nil)
	if *language != "" {
		opts = &runanywhere.TranscribeOptions{Language: *language}
	}

	resp, err := client.Transcribe(ctx, f, *audioPath, opts)
	if err != nil {
		if se, ok := err.(*runanywhere.ServerError); ok && se.StatusCode == 501 {
			log.Fatalf("Transcriptions not configured: %s (start server with --stt-model)", se.Message)
		}
		log.Fatalf("Transcribe: %v", err)
	}

	fmt.Println(resp.Text)
}
