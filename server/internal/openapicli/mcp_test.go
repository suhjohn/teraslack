package openapicli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestMCPServeSupportsContentLengthFraming(t *testing.T) {
	server := testMCPServer(t)
	request := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}`)
	input := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(request), request)

	var output bytes.Buffer
	if err := server.Serve(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}

	body, framing, err := readMCPFrame(bufio.NewReader(bytes.NewReader(output.Bytes())))
	if err != nil {
		t.Fatalf("readMCPFrame() error = %v", err)
	}
	if framing != mcpFramingContentLength {
		t.Fatalf("framing = %v", framing)
	}

	response := decodeMCPResponse(t, body)
	if response.ID != float64(1) {
		t.Fatalf("response.ID = %#v", response.ID)
	}
	assertInitializeResponse(t, response)
}

func TestMCPServeSupportsJSONLineFraming(t *testing.T) {
	server := testMCPServer(t)
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"codex-mcp-client","version":"0.118.0"}}}` + "\n"

	var output bytes.Buffer
	if err := server.Serve(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	if strings.Contains(output.String(), "Content-Length:") {
		t.Fatalf("expected newline-framed response, got %q", output.String())
	}

	response := decodeMCPResponse(t, bytes.TrimSpace(output.Bytes()))
	if response.ID != float64(1) {
		t.Fatalf("response.ID = %#v", response.ID)
	}
	assertInitializeResponse(t, response)
}

func TestMCPToolInputSchemaUsesOnlyRealAPIParameters(t *testing.T) {
	cli, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	op := findOperation(t, cli, "GET", "/conversations")
	schema := mcpToolInputSchema(op)
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T", schema["properties"])
	}
	if _, ok := properties["all"]; ok {
		t.Fatalf("unexpected synthetic all argument in MCP schema")
	}
	if _, ok := properties["cursor"]; !ok {
		t.Fatalf("expected real cursor parameter in MCP schema")
	}
}

func testMCPServer(t *testing.T) *mcpServer {
	t.Helper()
	cli, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	server, err := newMCPServer(cli)
	if err != nil {
		t.Fatalf("newMCPServer() error = %v", err)
	}
	return server
}

func findOperation(t *testing.T, cli *CLI, method, path string) *Operation {
	t.Helper()
	for _, group := range cli.groups {
		for _, op := range group.Operations {
			if op.Method == method && op.Path == path {
				return op
			}
		}
	}
	t.Fatalf("operation %s %s not found", method, path)
	return nil
}

func decodeMCPResponse(t *testing.T, data []byte) mcpResponse {
	t.Helper()
	var response mcpResponse
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	return response
}

func assertInitializeResponse(t *testing.T, response mcpResponse) {
	t.Helper()
	if response.Error != nil {
		t.Fatalf("response.Error = %+v", response.Error)
	}
	result, ok := response.Result.(map[string]any)
	if !ok {
		t.Fatalf("response.Result type = %T", response.Result)
	}
	if result["protocolVersion"] != mcpProtocolVersion {
		t.Fatalf("protocolVersion = %#v", result["protocolVersion"])
	}
}
