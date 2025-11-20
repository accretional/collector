package main

import (
	"context"
	"log"

	"github.com/yourorg/sdk"
	"github.com/yourorg/sdk/ui"
)

func main() {
	bridge := sdk.NewClient(nil) 
	ctx := context.Background()

	// 1. Construct the Request
	// We are asking an LLM to analyze some data and give us structured output.
	req := bridge.Post("https://api.openai.com/v1/chat/completions").
		Header("Authorization", "Bearer sk-123").
		JSONBody(map[string]any{
			"model": "gpt-4",
			"messages": []map[string]string{
				{"role": "system", "content": "You are a data analyst. Return JSON."},
				{"role": "user", "content": "Analyze the sentiment of: 'The server latency is high but uptime is perfect'."},
			},
		}).
		// 2. Configure Extraction
		// We extract specific fields from the deep JSON response.
		Extract("raw_text", "choices[0].message.content").
		Extract("model_ver", "model").
		Extract("tokens", "usage.total_tokens")

	// 3. Execute
	resp, err := req.Fetch(ctx)
	if err != nil {
		log.Fatal(err)
	}

	// 4. Transform & Render
	// Instead of printing to console, we construct a "Custom Component".
	// This keeps our Go code focused on data, while the Frontend handles the pixels.
	
	// We map the EXTRACTED data from the HTTP response directly into the PROPS of the UI component.
	bridge.Display().Render(ctx, ui.Custom("AIInsightCard", map[string]any{
		"title":    "Sentiment Analysis",
		"content":  resp.GetString("raw_text"), // The text from the LLM
		"severity": "warning",                  // Logic derived in Go
		"metadata": map[string]any{
			"model":  resp.GetString("model_ver"),
			"cost":   calculateCost(resp.GetInt("tokens")), // Helper function
			"latency": "120ms",
		},
		"actions": []string{"Retry", "View Logs"},
	}))
}

func calculateCost(tokens int64) float64 {
	return float64(tokens) * 0.00003
}
