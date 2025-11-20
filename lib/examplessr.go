package main

import (
	"context"
	"fmt"
	"log"

	"github.com/yourorg/sdk"
	"github.com/yourorg/sdk/ui"
)

// =============================================================================
// 1. Define your Server-Side Components
// =============================================================================

// InsightCardProps defines the data our component needs.
type InsightCardProps struct {
	Title    string
	Content  string
	Severity string // "info", "warning", "error"
	Metadata map[string]string
}

// InsightCard is our reusable Go component.
type InsightCard struct {
	Props InsightCardProps
}

// Render implements the ServerComponent interface.
func (c InsightCard) Render() (string, error) {
	// We define the HTML/Tailwind template right here in Go.
	// This gives us full control over the layout without touching Frontend code.
	const tmpl = `
	<div class="border rounded-lg p-4 mb-4 shadow-sm {{if eq .Severity "warning"}}border-yellow-400 bg-yellow-50{{else}}border-gray-200{{end}}">
		<div class="flex justify-between items-center mb-2">
			<h3 class="font-bold text-lg text-gray-800">{{.Title}}</h3>
			<span class="text-xs uppercase font-semibold tracking-wider {{if eq .Severity "warning"}}text-yellow-700{{end}}">
				{{.Severity}}
			</span>
		</div>
		<p class="text-gray-700 mb-4">{{.Content}}</p>
		
		<div class="bg-white/50 p-2 rounded text-sm grid grid-cols-2 gap-2">
			{{range $key, $val := .Metadata}}
				<div class="text-gray-500">{{$key}}:</div>
				<div class="text-right font-mono">{{$val}}</div>
			{{end}}
		</div>
	</div>
	`
	
	return ui.BaseComponent{Template: tmpl, Data: c.Props}.Render()
}

// =============================================================================
// 2. Main Execution
// =============================================================================

func main() {
	ctx := context.Background()
	bridge := sdk.NewClient(nil)

	// A. Fetch Data (same as before)
	req := bridge.Post("https://api.openai.com/v1/chat/completions").
		Header("Authorization", "Bearer sk-123").
		JSONBody(map[string]any{
			"model": "gpt-4",
			"messages": []map[string]string{
				{"role": "user", "content": "Analyze: 'Database CPU is at 99%'."},
			},
		}).
		Extract("text", "choices[0].message.content").
		Extract("model", "model")

	resp, err := req.Fetch(ctx)
	if err != nil {
		log.Fatal(err)
	}

	// B. Instantiate the Go Component
	// We map the RPC response to our Struct, not a generic map.
	card := InsightCard{
		Props: InsightCardProps{
			Title:    "Infrastructure Alert",
			Content:  resp.GetString("text"),
			Severity: "warning",
			Metadata: map[string]string{
				"Model":   resp.GetString("model"),
				"Source":  "Prometheus",
				"Region":  "us-east-1",
			},
		},
	}

	// C. Render to HTML String
	htmlString, err := card.Render()
	if err != nil {
		log.Fatal(err)
	}

	// D. Send raw HTML to the bridge
	// The frontend blindly injects this string into the DOM.
	bridge.Display().RenderHTML(ctx, htmlString)
}
