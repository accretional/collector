package main

import (
	"context"
	"fmt"
	"log"

	"github.com/yourorg/sdk"
	"github.com/yourorg/sdk/ui"
	httpv1 "github.com/yourorg/rpc/gen/http/v1"
)

// AuthMiddleware is a reusable configuration function.
// It acts like a "Mixin" for your builder.
func AuthMiddleware(token string) sdk.RequestModifier {
	return func(b *sdk.RequestBuilder) {
		b.Header("Authorization", "Bearer "+token)
		b.Header("Content-Type", "application/json")
	}
}

func main() {
	bridge := sdk.NewClient(nil) // nil for mock/example
	ctx := context.Background()

	// 1. Create a Base Request
	// This object is configured but not executed.
	baseReq := bridge.Post("https://api.openai.com/v1").
		Apply(AuthMiddleware("sk-123"))

	// 2. Fork the base request for a specific task (Chat Completion)
	// We use .Clone() so we don't modify baseReq.
	chatResp, err := baseReq.Clone().
		Header("X-Custom-Trace", "request-1"). // Add specific header
		JSONBody(map[string]any{
			"model": "gpt-4",
			"messages": []map[string]string{
				{"role": "user", "content": "What is the capital of France?"},
			},
		}).
		// We can chain multiple extractions:
		Extract("answer", "choices[0].message.content").
		Extract("tokens_used", "usage.total_tokens").
		Extract("model_version", "model").
		Fetch(ctx)

	if err != nil {
		log.Fatal(err)
	}

	// 3. Access the multiple extracted values
	fmt.Printf("Model: %s used %d tokens\n", 
		chatResp.GetString("model_version"), 
		chatResp.GetInt("tokens_used"),
	)

	bridge.Display().Render(ctx, ui.Markdown(chatResp.GetString("answer")))

	// 4. Reuse the SAME base request for a totally different task (Image Gen)
	// Since we cloned before, baseReq is still clean (points to /v1, has auth).
	// Note: We override the Target URI path manually here if the SDK supported it,
	// or we just assume the base was generic enough.
	// Let's assume we want to hit a different endpoint but keep the auth.
	
	// Ideally, your SDK might have a .Path() method to append to the URI, 
	// but here we'll just imagine a scenario where we scrape a website instead 
	// using the same client pattern.
	
	scrapeResp, _ := bridge.Get("https://news.ycombinator.com").
		ExtractHTML("top_story", ".titleline > a", httpv1.HTMLDecode_TEXT_CONTENT).
		ExtractHTML("top_link", ".titleline > a", httpv1.HTMLDecode_HREF).
		Fetch(ctx)

	bridge.Display().Render(ctx, ui.Table(
		[]string{"Title", "Link"},
		[][]string{{scrapeResp.GetString("top_story"), scrapeResp.GetString("top_link")}},
	))
}
