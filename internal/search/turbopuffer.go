// Package search provides the real TurbopufferClient implementation
// using the official turbopuffer-go SDK.
package search

import (
	"context"
	"encoding/json"
	"fmt"

	tp "github.com/turbopuffer/turbopuffer-go"
	"github.com/turbopuffer/turbopuffer-go/option"
	"github.com/turbopuffer/turbopuffer-go/packages/param"

	"github.com/suhjohn/teraslack/internal/service"
)

// Compile-time check that Client implements service.TurbopufferClient.
var _ service.TurbopufferClient = (*Client)(nil)

// Client wraps the official turbopuffer-go SDK and implements
// service.TurbopufferClient with namespace-sharded operations.
type Client struct {
	sdk    tp.Client
	prefix string // namespace prefix, e.g. "teraslack" -> "teraslack_01"
}

// NewClient creates a real TurbopufferClient.
// apiKey is the TURBOPUFFER_API_KEY. nsPrefix is prepended to every
// namespace name (e.g. "teraslack" -> namespaces "teraslack_00" ... "teraslack_ff").
func NewClient(apiKey, nsPrefix string, opts ...option.RequestOption) *Client {
	allOpts := append([]option.RequestOption{
		option.WithAPIKey(apiKey),
	}, opts...)
	return &Client{
		sdk:    tp.NewClient(allOpts...),
		prefix: nsPrefix,
	}
}

// fullNamespace returns the fully-qualified namespace name.
func (c *Client) fullNamespace(ns string) string {
	if c.prefix == "" {
		return ns
	}
	return fmt.Sprintf("%s_%s", c.prefix, ns)
}

// Upsert inserts or updates a document in the given namespace.
func (c *Client) Upsert(ctx context.Context, namespace string, id string, embedding []float32, metadata map[string]any) error {
	ns := c.sdk.Namespace(c.fullNamespace(namespace))

	row := tp.RowParam{
		"id":     id,
		"vector": embedding,
	}
	for k, v := range metadata {
		switch val := v.(type) {
		case string, int, int64, float64, bool:
			row[k] = val
		case []string:
			row[k] = val
		default:
			b, err := json.Marshal(val)
			if err != nil {
				return fmt.Errorf("marshal metadata %q: %w", k, err)
			}
			row[k] = string(b)
		}
	}

	_, err := ns.Write(ctx, tp.NamespaceWriteParams{
		UpsertRows:     []tp.RowParam{row},
		DistanceMetric: "cosine_distance",
	})
	if err != nil {
		return fmt.Errorf("turbopuffer write: %w", err)
	}
	return nil
}

// Delete removes a document from the given namespace by ID.
func (c *Client) Delete(ctx context.Context, namespace string, id string) error {
	ns := c.sdk.Namespace(c.fullNamespace(namespace))

	_, err := ns.Write(ctx, tp.NamespaceWriteParams{
		Deletes: []any{id},
	})
	if err != nil {
		return fmt.Errorf("turbopuffer delete: %w", err)
	}
	return nil
}

// Query performs a vector similarity search within a single namespace.
func (c *Client) Query(ctx context.Context, namespace string, embedding []float32, limit int, filters map[string]any) ([]service.VectorResult, error) {
	ns := c.sdk.Namespace(c.fullNamespace(namespace))

	params := tp.NamespaceQueryParams{
		TopK:   param.NewOpt(int64(limit)),
		RankBy: tp.NewRankByVector("vector", embedding),
	}

	// Build filter chain from metadata filters
	var filterExprs []tp.Filter
	for k, v := range filters {
		switch val := v.(type) {
		case string:
			filterExprs = append(filterExprs, tp.NewFilterEq(k, val))
		case []string:
			anySlice := make([]any, len(val))
			for i, s := range val {
				anySlice[i] = s
			}
			filterExprs = append(filterExprs, tp.NewFilterIn[any](k, anySlice))
		default:
			filterExprs = append(filterExprs, tp.NewFilterEq(k, v))
		}
	}
	if len(filterExprs) == 1 {
		params.Filters = filterExprs[0]
	} else if len(filterExprs) > 1 {
		params.Filters = tp.NewFilterAnd(filterExprs)
	}

	resp, err := ns.Query(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("turbopuffer query: %w", err)
	}

	results := make([]service.VectorResult, 0, len(resp.Rows))
	for _, row := range resp.Rows {
		id, _ := row["id"].(string)
		dist, _ := row["dist"].(float64)
		score := 1.0 - dist

		meta := make(map[string]any)
		for k, v := range row {
			if k == "id" || k == "dist" || k == "vector" {
				continue
			}
			if k == "data" {
				meta[k] = normalizeMetadataData(v)
				continue
			}
			meta[k] = v
		}

		results = append(results, service.VectorResult{
			ID:       id,
			Score:    score,
			Metadata: meta,
		})
	}

	return results, nil
}

func normalizeMetadataData(v any) any {
	switch data := v.(type) {
	case json.RawMessage:
		out := make(json.RawMessage, len(data))
		copy(out, data)
		return out
	case []byte:
		if json.Valid(data) {
			out := make(json.RawMessage, len(data))
			copy(out, data)
			return out
		}
		return string(data)
	case string:
		if json.Valid([]byte(data)) {
			out := make(json.RawMessage, len(data))
			copy(out, data)
			return out
		}
		return data
	default:
		return v
	}
}

// GetEmbedding generates an embedding vector for the given text.
// This is a deterministic hash-based placeholder. In production,
// replace with an external embedding API (OpenAI, Cohere, etc.).
func (c *Client) GetEmbedding(_ context.Context, text string) ([]float32, error) {
	dims := 256
	vec := make([]float32, dims)
	if text == "" {
		return vec, nil
	}

	for i, b := range []byte(text) {
		vec[i%dims] += float32(b) / 255.0
	}

	// L2 normalize
	var norm float32
	for _, v := range vec {
		norm += v * v
	}
	if norm > 0 {
		invNorm := 1.0 / sqrt32(norm)
		for i := range vec {
			vec[i] *= invNorm
		}
	}

	return vec, nil
}

func sqrt32(x float32) float32 {
	if x <= 0 {
		return 0
	}
	z := x
	for i := 0; i < 10; i++ {
		z = (z + x/z) / 2
	}
	return z
}
