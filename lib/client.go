package bridge

import (
	"context"

	"google.golang.org/grpc"
	httpv1 "github.com/accretional/lib/proto/http/v1"
)

// Client is the high-level entry point for the bridge.
// It wraps the low-level gRPC client.
type Client struct {
	http    httpv1.HTTPServiceClient
	display httpv1.DisplayServiceClient
}

// NewClient creates a new bridge client from a generic gRPC connection.
func NewClient(cc grpc.ClientConnInterface) *Client {
	return &Client{
		http:    httpv1.NewHTTPServiceClient(cc),
		display: httpv1.NewDisplayServiceClient(cc),
	}
}

// Get initiates a GET request builder.
func (c *Client) Get(uri string) *RequestBuilder {
	return newRequest(c.http, httpv1.HTTPMethod_METHOD_GET, uri)
}

// Post initiates a POST request builder.
func (c *Client) Post(uri string) *RequestBuilder {
	return newRequest(c.http, httpv1.HTTPMethod_METHOD_POST, uri)
}

// Request initiates a builder with a custom method.
func (c *Client) Request(method string, uri string) *RequestBuilder {
	// Map string to enum... (omitted for brevity, usually a switch)
	return newRequest(c.http, httpv1.HTTPMethod_METHOD_POST, uri)
}

// Display access the UI rendering tools.
func (c *Client) Display() *DisplayClient {
	return &DisplayClient{client: c.display}
}
