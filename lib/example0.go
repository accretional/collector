package main

import (
	"context"
	"log"

	"github.com/yourorg/sdk"
	"github.com/yourorg/sdk/ui"
)

func main() {
	// 1. Setup (Generic connection)
	bridge := sdk.NewClient(myGrpcConn)
	ctx := context.Background()

	// 2. Calling an LLM (Fluent API)
	// Notice we use a standard Go map for the body.
	// We also use Extract() so we don't have to parse the huge JSON response manually.
	resp, err := bridge.Post("https://api.openai.com/v1/chat/completions").
		Header("Authorization", "Bearer sk-123").
		JSONBody(map[string]any{
			"model": "gpt-4",
			"messages": []map[string]string{
				{"role": "user", "content": "Explain quantum physics briefly."},
			},
		}).
		Extract("answer", "choices[0].message.content"). // Magic extraction
		Fetch(ctx)

	if err != nil {
		log.Fatal(err)
	}

	// 3. Retrieving the extracted data
	answer := resp.GetString("answer")

	// 4. Rendering to the UI (The "Display" service)
	// We render the answer as Markdown.
	bridge.Display().Render(ctx, ui.Markdown(answer))

	// We can also dump the full raw response for debugging using the JSONTree component.
	// The SDK handles the conversion from the response object to the UI component.
	bridge.Display().Render(ctx, ui.JSONTree(resp.Raw()))
}
