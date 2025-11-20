package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/yourorg/sdk"
	"github.com/yourorg/sdk/ui"
)

// =============================================================================
// 1. The "Widget" Abstraction (Grouping Logic)
// =============================================================================

// Widget represents a self-contained UI feature.
// It knows where to render (TargetID) and how to get its data (Fetch).
type Widget struct {
	Title    string
	TargetID string
	Client   *sdk.Client
	Fetch    func(ctx context.Context, client *sdk.Client) (string, error)
}

// Mount renders the initial "Loading" state into the target slot.
func (w *Widget) Mount(ctx context.Context) {
	// Render a placeholder spinner immediately
	html, _ := ui.BaseComponent{
		Template: `
		<div id="{{.TargetID}}" class="border rounded p-4 h-48 flex flex-col justify-center items-center bg-gray-50">
			<div class="animate-spin h-6 w-6 border-4 border-blue-500 rounded-full border-t-transparent mb-2"></div>
			<span class="text-gray-400 text-sm">Loading {{.Title}}...</span>
		</div>`,
		Data: w,
	}.Render()

	// We use OUTER_HTML to replace whatever placeholder was in the layout
	w.Client.Display().RenderHTML(ctx, html)
}

// Refresh runs the fetch logic and updates the UI with either Data or Error.
func (w *Widget) Refresh(ctx context.Context) {
	// 1. Run the RPC (The heavy lifting)
	content, err := w.Fetch(ctx, w.Client)

	// 2. Handle Failure (User Influence)
	if err != nil {
		w.renderError(ctx, err)
		return
	}

	// 3. Handle Success (Hydration)
	w.renderSuccess(ctx, content)
}

func (w *Widget) renderSuccess(ctx context.Context, content string) {
	html, _ := ui.BaseComponent{
		Template: `
		<div id="{{.ID}}" class="border rounded p-4 h-48 bg-white shadow-sm transition-all duration-500 ease-in-out">
			<h3 class="font-bold text-gray-700 mb-2 border-b pb-2">{{.Title}}</h3>
			<div class="text-gray-800">{{.Content}}</div>
			<div class="mt-4 text-xs text-green-600 flex items-center">
				<span class="inline-block w-2 h-2 bg-green-500 rounded-full mr-2"></span> Live
			</div>
		</div>`,
		Data: map[string]string{
			"ID":      w.TargetID,
			"Title":   w.Title,
			"Content": content,
		},
	}.Render()

	// "Hydrate" the specific slot
	w.Client.Display().RenderHTML(ctx, html)
}

func (w *Widget) renderError(ctx context.Context, err error) {
	// Render an interactive error state
	html, _ := ui.BaseComponent{
		Template: `
		<div id="{{.ID}}" class="border border-red-200 rounded p-4 h-48 bg-red-50 flex flex-col justify-center items-center text-center">
			<h3 class="text-red-800 font-medium mb-1">Failed to load {{.Title}}</h3>
			<p class="text-red-600 text-xs mb-3">{{.Error}}</p>
			
			<!-- In a real app, this button would trigger a browser intent or callback -->
			<button class="px-3 py-1 bg-white border border-red-300 text-red-700 rounded text-sm hover:bg-red-100 transition">
				Retry Connection
			</button>
		</div>`,
		Data: map[string]string{
			"ID":    w.TargetID,
			"Title": w.Title,
			"Error": err.Error(),
		},
	}.Render()

	w.Client.Display().RenderHTML(ctx, html)
}

// =============================================================================
// 2. The Application Layout
// =============================================================================

func RenderAppSkeleton(ctx context.Context, bridge *sdk.Client) {
	layout := `
	<div class="max-w-4xl mx-auto p-6">
		<header class="mb-8">
			<h1 class="text-3xl font-bold text-gray-900">System Status</h1>
			<p class="text-gray-500">Real-time RPC Dashboard</p>
		</header>

		<!-- The Grid: These IDs match the Widget TargetIDs -->
		<div class="grid grid-cols-1 md:grid-cols-2 gap-6">
			<div id="slot-weather"></div>
			<div id="slot-stocks"></div>
			<div id="slot-news"></div>
			<div id="slot-servers"></div>
		</div>
	</div>
	`
	// Initial full-page render
	bridge.Display().RenderHTML(ctx, layout)
}

// =============================================================================
// 3. Main Orchestration
// =============================================================================

func main() {
	ctx := context.Background()
	bridge := sdk.NewClient(nil)

	// A. Initialize the "Unpopulated" View
	RenderAppSkeleton(ctx, bridge)

	// B. Define our Widgets (Grouped Logic)
	widgets := []*Widget{
		{
			Title:    "Weather API",
			TargetID: "slot-weather",
			Client:   bridge,
			Fetch: func(ctx context.Context, c *sdk.Client) (string, error) {
				// Simulate RPC
				resp, err := c.Get("https://api.weather.gov/points/39.7456,-97.0892").Fetch(ctx)
				if err != nil { return "", err }
				// Mock return for demo if API fails/mocked
				return "Temperature: 72°F\nCondition: Sunny", nil
			},
		},
		{
			Title:    "Stock Ticker",
			TargetID: "slot-stocks",
			Client:   bridge,
			Fetch: func(ctx context.Context, c *sdk.Client) (string, error) {
				// Simulate slow RPC
				time.Sleep(2 * time.Second)
				return "AAPL: $150.00 (+1.2%)\nGOOGL: $2800.00 (-0.5%)", nil
			},
		},
		{
			Title:    "Unstable Service",
			TargetID: "slot-servers",
			Client:   bridge,
			Fetch: func(ctx context.Context, c *sdk.Client) (string, error) {
				// Simulate a failure to demonstrate Error UI
				time.Sleep(1 * time.Second)
				return "", fmt.Errorf("timeout waiting for gateway")
			},
		},
	}

	// C. Fire off RPCs (Hydration)
	var wg sync.WaitGroup
	for _, w := range widgets {
		wg.Add(1)
		w := w // Capture loop variable
		
		go func() {
			defer wg.Done()
			
			// 1. Render Loading State immediately
			w.Mount(ctx)

			// 2. Perform Async Fetch & Update
			w.Refresh(ctx)
		}()
	}

	// Keep main alive until all UI updates are pushed
	wg.Wait()
	log.Println("Dashboard hydration complete.")
}
