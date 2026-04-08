package openapicli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
)

const mcpProtocolVersion = "2025-03-26"

type mcpFraming int

const (
	mcpFramingContentLength mcpFraming = iota
	mcpFramingJSONLine
)

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *mcpError `json:"error,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpTool struct {
	Name        string
	Description string
	InputSchema map[string]any
	Operation   *Operation
}

type mcpServer struct {
	cli    *CLI
	tools  []*mcpTool
	byName map[string]*mcpTool
}

func RunMCPServer(ctx context.Context, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	mcpDebugf("run start")
	cli, err := New()
	if err != nil {
		mcpDebugf("new cli error: %v", err)
		fmt.Fprintf(stderr, "load openapi cli: %v\n", err)
		return 1
	}
	server, err := newMCPServer(cli)
	if err != nil {
		mcpDebugf("new server error: %v", err)
		fmt.Fprintf(stderr, "build MCP server: %v\n", err)
		return 1
	}
	mcpDebugf("serve start tools=%d", len(server.tools))
	if err := server.Serve(ctx, stdin, stdout); err != nil {
		mcpDebugf("serve error: %v", err)
		fmt.Fprintf(stderr, "run MCP server: %v\n", err)
		return 1
	}
	mcpDebugf("serve exit clean")
	return 0
}

func newMCPServer(cli *CLI) (*mcpServer, error) {
	tools := buildMCPTools(cli)
	byName := make(map[string]*mcpTool, len(tools))
	for _, tool := range tools {
		byName[tool.Name] = tool
	}
	return &mcpServer{
		cli:    cli,
		tools:  tools,
		byName: byName,
	}, nil
}

func buildMCPTools(cli *CLI) []*mcpTool {
	tools := make([]*mcpTool, 0, cli.operationCnt)
	for _, group := range cli.groups {
		for _, op := range group.Operations {
			tools = append(tools, &mcpTool{
				Name:        mcpToolName(op),
				Description: mcpToolDescription(op),
				InputSchema: mcpToolInputSchema(op),
				Operation:   op,
			})
		}
	}
	sort.Slice(tools, func(i, j int) bool {
		return tools[i].Name < tools[j].Name
	})
	return tools
}

func mcpToolName(op *Operation) string {
	return strings.ReplaceAll(op.GroupName, "-", "_") + "_" + strings.ReplaceAll(op.Name, "-", "_")
}

func mcpToolDescription(op *Operation) string {
	about := firstNonEmpty(strings.TrimSpace(op.Summary), strings.TrimSpace(op.Description), op.Method+" "+op.Path)
	return fmt.Sprintf("%s. Mirrors `teraslack %s %s` and runs with the session-specific Teraslack agent identity when auth is required.", oneLine(about), op.GroupName, op.Name)
}

func mcpToolInputSchema(op *Operation) map[string]any {
	properties := map[string]any{}
	required := make([]string, 0, len(op.Parameters))

	for _, param := range op.Parameters {
		schema := schemaRefToJSONSchema(param.Schema, map[string]bool{})
		if description := strings.TrimSpace(param.Description); description != "" {
			schema["description"] = description
		} else {
			schema["description"] = fmt.Sprintf("%s %s parameter.", param.In, param.Name)
		}
		properties[param.FlagName] = schema
		if param.Required {
			required = append(required, param.FlagName)
		}
	}

	if op.RequestBody != nil {
		bodySchema := schemaRefToJSONSchema(op.RequestBody, map[string]bool{})
		bodySchema["description"] = "Full JSON request body."
		properties["body"] = bodySchema
	}

	for _, field := range op.BodyFields {
		schema := schemaRefToJSONSchema(field.Schema, map[string]bool{})
		if description := strings.TrimSpace(field.Description); description != "" {
			schema["description"] = description
		} else {
			schema["description"] = fmt.Sprintf("Request body field %s.", strings.Join(field.Path, "."))
		}
		properties[field.FlagName] = schema
	}

	inputSchema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           properties,
	}
	if len(required) > 0 {
		inputSchema["required"] = required
	}
	return inputSchema
}

func schemaRefToJSONSchema(ref *openapi3.SchemaRef, seen map[string]bool) map[string]any {
	if ref == nil {
		return map[string]any{"type": "object"}
	}
	if ref.Ref != "" {
		if seen[ref.Ref] {
			return map[string]any{"type": "object"}
		}
		seen[ref.Ref] = true
	}

	schema := derefSchema(ref)
	if schema == nil {
		return map[string]any{"type": "object"}
	}

	if len(schema.OneOf) > 0 {
		values := make([]any, 0, len(schema.OneOf))
		for _, item := range schema.OneOf {
			values = append(values, schemaRefToJSONSchema(item, cloneSeenRefs(seen)))
		}
		return withSchemaMetadata(schema, map[string]any{"oneOf": values})
	}
	if len(schema.AnyOf) > 0 {
		values := make([]any, 0, len(schema.AnyOf))
		for _, item := range schema.AnyOf {
			values = append(values, schemaRefToJSONSchema(item, cloneSeenRefs(seen)))
		}
		return withSchemaMetadata(schema, map[string]any{"anyOf": values})
	}
	if len(schema.AllOf) > 0 {
		values := make([]any, 0, len(schema.AllOf))
		for _, item := range schema.AllOf {
			values = append(values, schemaRefToJSONSchema(item, cloneSeenRefs(seen)))
		}
		return withSchemaMetadata(schema, map[string]any{"allOf": values})
	}

	out := map[string]any{}
	switch {
	case schema.Type != nil && schema.Type.Is("object"):
		out["type"] = "object"
		properties := map[string]any{}
		names := make([]string, 0, len(schema.Properties))
		for name := range schema.Properties {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			properties[name] = schemaRefToJSONSchema(schema.Properties[name], cloneSeenRefs(seen))
		}
		out["properties"] = properties
		if len(schema.Required) > 0 {
			out["required"] = append([]string(nil), schema.Required...)
		}
		if schema.AdditionalProperties.Has != nil {
			out["additionalProperties"] = *schema.AdditionalProperties.Has
		} else if schema.AdditionalProperties.Schema != nil {
			out["additionalProperties"] = schemaRefToJSONSchema(schema.AdditionalProperties.Schema, cloneSeenRefs(seen))
		}
	case schema.Type != nil && schema.Type.Is("array"):
		out["type"] = "array"
		out["items"] = schemaRefToJSONSchema(schema.Items, cloneSeenRefs(seen))
	case schema.Type != nil && schema.Type.Is("boolean"):
		out["type"] = "boolean"
	case schema.Type != nil && schema.Type.Is("integer"):
		out["type"] = "integer"
	case schema.Type != nil && schema.Type.Is("number"):
		out["type"] = "number"
	case schema.Type != nil && schema.Type.Is("string"):
		out["type"] = "string"
	default:
		out["type"] = "object"
	}
	return withSchemaMetadata(schema, out)
}

func withSchemaMetadata(schema *openapi3.Schema, target map[string]any) map[string]any {
	if description := strings.TrimSpace(schema.Description); description != "" {
		target["description"] = description
	}
	if format := strings.TrimSpace(schema.Format); format != "" {
		target["format"] = format
	}
	if len(schema.Enum) > 0 {
		target["enum"] = append([]any(nil), schema.Enum...)
	}
	if schema.Nullable {
		target["nullable"] = true
	}
	return target
}

func cloneSeenRefs(input map[string]bool) map[string]bool {
	out := make(map[string]bool, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func (s *mcpServer) Serve(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	reader := bufio.NewReader(stdin)
	writer := bufio.NewWriter(stdout)
	mcpDebugf("serve loop enter")

	for {
		body, framing, err := readMCPFrame(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				mcpDebugf("serve loop eof")
				return nil
			}
			mcpDebugf("serve loop read error: %v", err)
			return err
		}
		mcpDebugf("serve loop read frame bytes=%d", len(body))

		var request mcpRequest
		if err := json.Unmarshal(body, &request); err != nil {
			mcpDebugf("request decode error: %v", err)
			if writeErr := writeMCPFrame(writer, framing, mcpResponse{
				JSONRPC: "2.0",
				Error:   &mcpError{Code: -32700, Message: "parse error"},
			}); writeErr != nil {
				mcpDebugf("write parse-error response failed: %v", writeErr)
				return writeErr
			}
			continue
		}
		mcpDebugf("request method=%q id=%v", request.Method, request.ID)

		response, shouldRespond := s.handleRequest(ctx, request)
		if !shouldRespond {
			mcpDebugf("request method=%q handled without response", request.Method)
			continue
		}
		if err := writeMCPFrame(writer, framing, response); err != nil {
			mcpDebugf("write response error: %v", err)
			return err
		}
		mcpDebugf("response written method=%q id=%v", request.Method, request.ID)
	}
}

func (s *mcpServer) handleRequest(ctx context.Context, request mcpRequest) (mcpResponse, bool) {
	switch request.Method {
	case "initialize":
		return mcpResponse{
			JSONRPC: "2.0",
			ID:      request.ID,
			Result: map[string]any{
				"protocolVersion": mcpProtocolVersion,
				"capabilities": map[string]any{
					"tools": map[string]any{
						"listChanged": false,
					},
				},
				"serverInfo": map[string]any{
					"name":    "teraslack",
					"version": strings.TrimSpace(Version),
				},
				"instructions": "This server mirrors the Teraslack CLI/API surface. Tool names match `teraslack <group> <command>` as `<group>_<command>` and run with the session-specific Teraslack agent identity.",
			},
		}, true
	case "notifications/initialized", "initialized", "$/cancelRequest":
		return mcpResponse{}, false
	case "ping":
		return mcpResponse{JSONRPC: "2.0", ID: request.ID, Result: map[string]any{}}, true
	case "tools/list":
		return mcpResponse{
			JSONRPC: "2.0",
			ID:      request.ID,
			Result: map[string]any{
				"tools": s.describeTools(),
			},
		}, true
	case "tools/call":
		result, err := s.handleToolCall(ctx, request.Params)
		if err != nil {
			return mcpResponse{
				JSONRPC: "2.0",
				ID:      request.ID,
				Result: map[string]any{
					"content": []map[string]any{
						{"type": "text", "text": err.Error()},
					},
					"isError": true,
				},
			}, true
		}
		return mcpResponse{JSONRPC: "2.0", ID: request.ID, Result: result}, true
	default:
		return mcpResponse{
			JSONRPC: "2.0",
			ID:      request.ID,
			Error:   &mcpError{Code: -32601, Message: "method not found"},
		}, true
	}
}

func (s *mcpServer) describeTools() []map[string]any {
	tools := make([]map[string]any, 0, len(s.tools))
	for _, tool := range s.tools {
		tools = append(tools, map[string]any{
			"name":        tool.Name,
			"description": tool.Description,
			"inputSchema": tool.InputSchema,
		})
	}
	return tools
}

func (s *mcpServer) handleToolCall(ctx context.Context, params json.RawMessage) (map[string]any, error) {
	var call struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &call); err != nil {
		return nil, fmt.Errorf("decode tool call: %w", err)
	}

	tool := s.byName[strings.TrimSpace(call.Name)]
	if tool == nil {
		return nil, fmt.Errorf("unknown Teraslack tool %q", call.Name)
	}

	args := call.Arguments
	if args == nil {
		args = map[string]any{}
	}

	path, query, body, err := buildMCPRequest(tool.Operation, args)
	if err != nil {
		return nil, err
	}

	baseURL := ""
	authToken := ""
	if tool.Operation.RequiresAuth {
		record, err := loadCurrentAgentSessionRecord()
		if err != nil {
			return nil, fmt.Errorf("%s. Start Codex or Claude in a linked directory and let the Teraslack SessionStart hook initialize the session agent", err)
		}
		baseURL = canonicalBaseURL(record.BaseURL)
		authToken = strings.TrimSpace(record.AgentToken)
	} else {
		cfg, err := loadFileConfig()
		if err != nil {
			return nil, fmt.Errorf("load CLI config: %w", err)
		}
		baseURL = canonicalBaseURL(firstNonEmpty(cfg.BaseURL, defaultAuthBaseURL))
	}

	if strings.TrimSpace(baseURL) == "" {
		return nil, fmt.Errorf("missing Teraslack API base URL")
	}

	response, err := executeOperation(ctx, tool.Operation.Method, baseURL, authToken, path, query, body)
	if err != nil {
		return nil, err
	}

	result := map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": renderMCPResultText(response)},
		},
	}
	if response != nil {
		result["structuredContent"] = response
	}
	return result, nil
}

func buildMCPRequest(op *Operation, args map[string]any) (string, map[string]any, any, error) {
	path := op.Path
	query := map[string]any{}
	var body any
	if value, ok := args["body"]; ok {
		body = cloneJSON(value)
	}

	for _, param := range op.Parameters {
		value, ok := args[param.FlagName]
		if !ok || !isProvidedMCPValue(value) {
			if param.Required {
				return "", nil, nil, fmt.Errorf("missing required argument %q", param.FlagName)
			}
			continue
		}
		switch param.In {
		case "path":
			path = strings.ReplaceAll(path, "{"+param.Name+"}", fmt.Sprint(value))
		case "query":
			if slice, ok := value.([]any); ok {
				query[param.Name] = joinCSV(slice)
			} else {
				query[param.Name] = value
			}
		default:
			return "", nil, nil, fmt.Errorf("unsupported parameter location %q", param.In)
		}
	}

	for _, field := range op.BodyFields {
		value, ok := args[field.FlagName]
		if !ok || !isProvidedMCPValue(value) {
			continue
		}
		objectBody, err := ensureObjectBody(body, map[string]*string{field.FlagName: stringPtr("present")})
		if err != nil {
			return "", nil, nil, err
		}
		applySet(objectBody, strings.Join(field.Path, "."), cloneJSON(value))
		body = objectBody
	}

	if len(op.BodyFields) > 0 || body != nil {
		if err := validateRequiredBodyFields(body, op.BodyFields); err != nil {
			return "", nil, nil, fmt.Errorf("%s", strings.ReplaceAll(err.Error(), "flag", "argument"))
		}
	}

	return path, query, body, nil
}

func isProvidedMCPValue(value any) bool {
	if value == nil {
		return false
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text) != ""
	}
	return true
}

func renderMCPResultText(value any) string {
	if value == nil {
		return "null"
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(data)
}

func readMCPFrame(reader *bufio.Reader) ([]byte, mcpFraming, error) {
	contentLength := 0
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) && line == "" {
				mcpDebugf("read frame eof before header")
				return nil, mcpFramingContentLength, io.EOF
			}
			mcpDebugf("read header error line=%q err=%v", line, err)
			return nil, mcpFramingContentLength, err
		}
		trimmed := strings.TrimRight(line, "\r\n")
		mcpDebugf("read header line=%q", trimmed)
		stripped := strings.TrimSpace(trimmed)
		if stripped == "" {
			break
		}
		if contentLength == 0 && isMCPJSONLine(stripped) {
			return []byte(stripped), mcpFramingJSONLine, nil
		}
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			return nil, mcpFramingContentLength, fmt.Errorf("invalid MCP header %q", trimmed)
		}
		if strings.EqualFold(strings.TrimSpace(key), "Content-Length") {
			fmt.Sscanf(strings.TrimSpace(value), "%d", &contentLength)
		}
	}
	if contentLength <= 0 {
		mcpDebugf("read frame missing content length")
		return nil, mcpFramingContentLength, fmt.Errorf("missing Content-Length header")
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, body); err != nil {
		mcpDebugf("read body error bytes=%d err=%v", contentLength, err)
		return nil, mcpFramingContentLength, err
	}
	return body, mcpFramingContentLength, nil
}

func isMCPJSONLine(value string) bool {
	return strings.HasPrefix(value, "{") && json.Valid([]byte(value))
}

func mcpDebugf(format string, args ...any) {
	path := strings.TrimSpace(os.Getenv("TERASLACK_MCP_DEBUG_LOG"))
	if path == "" {
		return
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	fmt.Fprintf(file, "%s ", time.Now().Format(time.RFC3339Nano))
	fmt.Fprintf(file, format, args...)
	fmt.Fprintln(file)
}

func writeMCPFrame(writer *bufio.Writer, framing mcpFraming, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if framing == mcpFramingJSONLine {
		if _, err := writer.Write(data); err != nil {
			return err
		}
		if err := writer.WriteByte('\n'); err != nil {
			return err
		}
		return writer.Flush()
	}
	if _, err := fmt.Fprintf(writer, "Content-Length: %d\r\n\r\n", len(data)); err != nil {
		return err
	}
	if _, err := writer.Write(data); err != nil {
		return err
	}
	return writer.Flush()
}
