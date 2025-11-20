package ui

import (
	"bytes"
	"html/template"
)

// ServerComponent is an interface for anything that can turn itself into HTML.
type ServerComponent interface {
	Render() (string, error)
}

// BaseComponent helps clean up template execution.
type BaseComponent struct {
	Template string
	Data     any
}

func (b BaseComponent) Render() (string, error) {
	// Parse the template
	// In production, you might cache these templates globally using template.ParseFiles
	// instead of parsing on every render.
	tmpl, err := template.New("component").Parse(b.Template)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, b.Data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// Fragment stitches multiple components together into one string.
func Fragment(components ...ServerComponent) (string, error) {
	var buf bytes.Buffer
	for _, c := range components {
		html, err := c.Render()
		if err != nil {
			return "", err
		}
		buf.WriteString(html)
	}
	return buf.String(), nil
}
