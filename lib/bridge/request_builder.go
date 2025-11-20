package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	httpv1 "github.com/accretional/lib/bridge/http/v1"
)

// RequestBuilder allows constructing complex requests fluently.
type RequestBuilder struct {
	client  httpv1.HTTPServiceClient
	req     *httpv1.HTTPRequest
	err     error
}

// RequestModifier is a function that applies common logic to a builder.
type RequestModifier func(*RequestBuilder)

func newRequest(client httpv1.HTTPServiceClient, method httpv1.HTTPMethod, uri string) *RequestBuilder {
	return &RequestBuilder{
		client: client,
		req: &httpv1.HTTPRequest{
			Method: method,
			Target: &httpv1.Target{Uri: uri},
			Options: &httpv1.RequestOptions{
				SimpleHeaders: make(map[string]string),
			},
			Decode: &httpv1.HTTPDecode{
				JsonExtract: make(map[string]string),
				HtmlExtract: make(map[string]*httpv1.HTMLDecode),
			},
		},
	}
}

// Clone creates a deep copy of the current builder.
// This is essential for creating "Base Requests" and reusing them.
func (b *RequestBuilder) Clone() *RequestBuilder {
	// proto.Clone performs a deep copy of the underlying message
	newReq := proto.Clone(b.req).(*httpv1.HTTPRequest)
	return &RequestBuilder{
		client: b.client,
		req:    newReq,
		err:    b.err,
	}
}

// Apply runs a function against the builder.
// Useful for applying shared configuration (auth, logging, standard headers).
func (b *RequestBuilder) Apply(mods ...RequestModifier) *RequestBuilder {
	for _, mod := range mods {
		mod(b)
	}
	return b
}

// Header adds a simple header.
func (b *RequestBuilder) Header(key, value string) *RequestBuilder {
	b.req.Options.SimpleHeaders[key] = value
	return b
}

// JSONBody sets the payload.
func (b *RequestBuilder) JSONBody(payload any) *RequestBuilder {
	if b.err != nil { return b }

	bytes, err := json.Marshal(payload)
	if err != nil {
		b.err = fmt.Errorf("failed to marshal payload: %w", err)
		return b
	}

	s := &structpb.Struct{}
	if err := s.UnmarshalJSON(bytes); err != nil {
		b.err = fmt.Errorf("failed to unmarshal into proto struct: %w", err)
		return b
	}

	b.req.Body = &httpv1.Body{
		Payload: &httpv1.Body_JsonData{JsonData: s},
	}
	return b
}

// Extract tells the bridge to parse specific fields from the JSON response.
// You can call this multiple times to extract multiple fields.
func (b *RequestBuilder) Extract(key, jsonPath string) *RequestBuilder {
	b.req.Decode.JsonExtract[key] = jsonPath
	return b
}

// ExtractHTML allows scraping data from the response.
func (b *RequestBuilder) ExtractHTML(key, selector string, attr httpv1.HTMLDecode_Attribute) *RequestBuilder {
	b.req.Decode.HtmlExtract[key] = &httpv1.HTMLDecode{
		Selector: selector,
		Attribute: attr,
	}
	return b
}

// Fetch executes the request.
func (b *RequestBuilder) Fetch(ctx context.Context) (*Response, error) {
	if b.err != nil {
		return nil, b.err
	}

	resp, err := b.client.Fetch(ctx, b.req)
	if err != nil {
		return nil, err
	}

	return &Response{raw: resp}, nil
}

// --- Response Helpers ---

type Response struct {
	raw *httpv1.HTTPResponse
}

// GetString retrieves a string value extracted via .Extract()
func (r *Response) GetString(key string) string {
	if v, ok := r.raw.DecodedData[key]; ok {
		return v.GetStringValue()
	}
	return ""
}

// GetInt retrieves a number value extracted via .Extract()
func (r *Response) GetInt(key string) int64 {
	if v, ok := r.raw.DecodedData[key]; ok {
		// Handle actual numbers or strings that look like numbers
		if n := v.GetNumberValue(); n != 0 {
			return int64(n)
		}
		if s := v.GetStringValue(); s != "" {
			i, _ := strconv.ParseInt(s, 10, 64)
			return i
		}
	}
	return 0
}

// GetStruct retrieves a nested JSON object extracted via .Extract()
func (r *Response) GetStruct(key string) *structpb.Struct {
	if v, ok := r.raw.DecodedData[key]; ok {
		return v.GetStructValue()
	}
	return nil
}

// Raw returns the underlying body.
func (r *Response) Raw() []byte {
	if r.raw.Body == nil { return nil }
	return r.raw.Body.GetRawBytes()
}
