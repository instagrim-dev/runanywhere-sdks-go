// device-chat runs on-device LLM inference using the RunAnywhere Go device package.
// It performs one non-streaming Generate and one GenerateStream, then shuts down.
//
// Prerequisites:
//  1. Build runanywhere-commons shared libs (see README in this directory or sdk/runanywhere-go/README.md).
//  2. Set CGO_CPPFLAGS and CGO_LDFLAGS (or RAC_COMMONS_INCLUDE / RAC_COMMONS_LIB) and build with CGO_ENABLED=1.
//  3. At runtime, set LD_LIBRARY_PATH (Linux) or DYLD_LIBRARY_PATH (macOS) so the loader finds the .so/.dylib.
//
// Run: go run . [--model=/path/to/model.gguf]
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"

	"github.com/runanywhere/runanywhere-sdks-go/sdk/runanywhere-go/device"
)

func main() {
	modelPath := flag.String("model", "", "Path to GGUF model (required)")
	flag.Parse()

	if *modelPath == "" {
		log.Fatal("Usage: go run . --model=/path/to/model.gguf")
	}

	ctx := context.Background()

	if err := device.Init(ctx); err != nil {
		if errors.Is(err, device.ErrUnsupported) {
			log.Fatalf("On-device inference requires CGO and runanywhere-commons shared libraries. "+
				"Build the commons with --shared (e.g. scripts/build-linux.sh --shared llamacpp), "+
				"set CGO_CPPFLAGS/CGO_LDFLAGS, build with CGO_ENABLED=1, and set LD_LIBRARY_PATH at runtime. See README.")
		}
		log.Fatalf("Init: %v", err)
	}
	defer func() {
		if err := device.Shutdown(); err != nil {
			log.Printf("Shutdown: %v", err)
		}
	}()

	llm, err := device.NewLLM(ctx, *modelPath, nil)
	if err != nil {
		if errors.Is(err, device.ErrUnsupported) {
			log.Fatalf("Device package is built as stubs (CGO_ENABLED=0 or libs not linked). See README.")
		}
		log.Fatalf("NewLLM: %v", err)
	}
	defer llm.Close()

	// Non-streaming
	fmt.Println("--- Generate (non-streaming) ---")
	out, err := llm.Generate(ctx, "Say hello in one short sentence.", nil)
	if err != nil {
		log.Fatalf("Generate: %v", err)
	}
	fmt.Println(out)

	// Streaming
	fmt.Println("\n--- GenerateStream ---")
	it, err := llm.GenerateStream(ctx, "Count from 1 to 5.", nil)
	if err != nil {
		log.Fatalf("GenerateStream: %v", err)
	}
	defer it.Close()
	for {
		token, isFinal, err := it.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("Next: %v", err)
		}
		fmt.Print(token)
		if isFinal {
			break
		}
	}
	fmt.Println()
	fmt.Println("Done.")
}
