package main

import (
	"context"
	"fmt"
	"log"

	"github.com/yourorg/sdk"
	"github.com/yourorg/sdk/ui"
)

// =============================================================================
// 1. Base Factories (Configuration)
// =============================================================================

// NewOpenAIClient returns a RequestBuilder pre-configured with Auth and Base URL.
// It is a "Template" that can be reused for ANY OpenAI request.
func NewOpenAIClient(client *sdk.Client, token string) *sdk.RequestBuilder {
	return client.Post("https://api.openai.com/v1/chat/completions").
		Header("Authorization", "Bearer "+token).
		Header("Content-Type", "application/json")
}

// =============================================================================
// 2. Mixins (Shared Logic)
// =============================================================================

// WithJSONMode is a functional mixin that forces the LLM to reply in JSON.
// It matches the sdk.RequestModifier signature: func(*RequestBuilder)
func WithJSONMode(b *sdk.RequestBuilder) {
	// We can modify the body, headers, or query params here
	b.Header("X-Response-Format", "json_object") 
	// (In reality you'd edit the JSON body here, simplified for demo)
}

// WithHighPriority adds headers for priority queueing.
func WithHighPriority(b *sdk.RequestBuilder) {
	b.Header("X-Priority", "high")
}

// =============================================================================
// 3. Partial Builders (Domain Logic)
// =============================================================================

// NewAnalysisRequest constructs a request for a specific business task,
// but DOES NOT execute it. It returns the builder so the caller can tweak it.
func NewAnalysisRequest(base *sdk.RequestBuilder, text string) *sdk.RequestBuilder {
	// Clone the base so we don't mess up the original "client" template
	req := base.Clone()

	// Apply our Mixins
	req.Apply(WithJSONMode, WithHighPriority)

	// Set the specific payload
	req.JSONBody(map[string]any{
		"model": "gpt-4",
		"messages": []map[string]string{
			{"role": "user", "content": fmt.Sprintf("Analyze this: %s", text)},
		},
	})

	return req
}

// =============================================================================
// 4. Result Wrappers (Type Safety)
// =============================================================================

// AnalysisResult is a strictly typed Go struct for our domain.
type AnalysisResult struct {
	Summary   string
	Sentiment string
	Score     int64
}

// FetchAnalysis wraps the execution logic.
// It takes a generic builder, adds the specific extractions needed for this struct,
// executes it, and maps the result.
func FetchAnalysis(ctx context.Context, req *sdk.RequestBuilder) (*AnalysisResult, error) {
	// The Caller (this function) decides what to extract, 
	// even though the Request was built elsewhere.
	resp, err := req.
		Extract("summary", "choices[0].message.content.summary").
		Extract("sentiment", "choices[0].message.content.sentiment").
		Extract("score", "choices[0].message.content.confidence_score").
		Fetch(ctx)

	if err != nil {
		return nil, err
	}

	return &AnalysisResult{
		Summary:   resp.GetString("summary"),
		Sentiment: resp.GetString("sentiment"),
		Score:     resp.GetInt("score"),
	}, nil
}

// =============================================================================
// Main Execution
// =============================================================================

func main() {
	ctx := context.Background()
	bridge := sdk.NewClient(nil) // Mock client

	// A. Create the reusable "Base"
	openAIBase := NewOpenAIClient(bridge, "sk-123")

	// B. Create a specific request using the base
	// Notice we are just passing pointers around, no network calls yet.
	analysisReq := NewAnalysisRequest(openAIBase, "The server is slow but stable.")

	// C. (Optional) The "Caller" can perform last-minute overrides
	analysisReq.Header("X-Trace-ID", "trace-999")

	// D. Execute and Map using our wrapper
	result, err := FetchAnalysis(ctx, analysisReq)
	if err != nil {
		log.Fatal(err)
	}

	// E. Use the typed result
	fmt.Printf("Sentiment: %s (Score: %d)\n", result.Sentiment, result.Score)

	// F. Reuse the base for a totally different request
	// Since we Cloned inside NewAnalysisRequest, openAIBase is still clean.
	// Here we do a raw fetch without the wrapper.
	rawResp, _ := openAIBase.Clone().
		JSONBody(map[string]string{"msg": "hello"}).
		Extract("reply", "choices[0].message.content").
		Fetch(ctx)
	
	bridge.Display().Render(ctx, ui.Markdown(rawResp.GetString("reply")))
}
