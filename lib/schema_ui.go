package main

import (
	"context"
	"log"

	"github.com/yourorg/reflection" // The package we just wrote
	"github.com/yourorg/sdk/ui"     // Your UI SDK
	
	// The tenant's service code
	computev1 "github.com/yourorg/rpc/gen/compute/v1"
)

func main() {
	ctx := context.Background()
	
	// 1. Instantiate the "Empty" Request object
	// We don't need to fill it, we just need its type information.
	emptyRequest := &computev1.CreateInstanceRequest{}

	// 2. Introspect (Reflection)
	// Extract the metadata burned into the binary.
	archetypes, _ := reflection.IntrospectArchetypes(emptyRequest)
	traits, _ := reflection.IntrospectTraits(emptyRequest)

	// 3. Build Dynamic UI (Server-Driven)
	// We construct a generic "Selection Form" based on the archetypes.
	
	var options []string
	for _, arch := range archetypes {
		// Format: "Micro - A cheap instance..."
		label := arch.Name + " - " + arch.Description
		options = append(options, label)
	}

	// Create the component using our SDK
	// This works generically for ANY resource in your system.
	formComponent := ui.Custom("ResourceWizard", map[string]any{
		"title": "Create New Resource",
		"archetypes": options,
		"field_hints": traits, // e.g. "zone" -> "geographic_location" (UI renders a map)
	})

	// 4. Send to Frontend
	// Assuming we have a bridge client...
	// bridge.Display().Render(ctx, formComponent)
	
	log.Printf("Generated UI for %d archetypes and %d traits", len(archetypes), len(traits))
}
