package openapicli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/johnsuh/teraslack/server/internal/api"
)

const defaultAuthBaseURL = "https://api.teraslack.ai"

type CLI struct {
	groups       []*Group
	groupByName  map[string]*Group
	operationCnt int
	routes       []string
}

type topLevelAlias struct {
	Group   string
	Command string
}

type Group struct {
	DisplayName string
	Name        string
	Description string
	Operations  []*Operation
	byName      map[string]*Operation
}

type Operation struct {
	GroupDisplayName string
	GroupName        string
	Name             string
	OperationID      string
	Method           string
	Path             string
	Summary          string
	Description      string
	Parameters       []Parameter
	BodyFields       []BodyField
	RequestBody      *openapi3.SchemaRef
	RequiresAuth     bool
	CursorField      string
	CursorLocation   string
}

type Parameter struct {
	Name        string
	FlagName    string
	In          string
	Description string
	Required    bool
	Schema      *openapi3.SchemaRef
}

type BodyField struct {
	Path        []string
	FlagName    string
	Description string
	Required    bool
	Schema      *openapi3.SchemaRef
}

type stringValues []string

type fileConfig struct {
	BaseURL      string `json:"base_url"`
	APIKey       string `json:"api_key,omitempty"`
	SessionToken string `json:"session_token,omitempty"`
	WorkspaceID  string `json:"workspace_id,omitempty"`
	UserID       string `json:"user_id,omitempty"`
}

var topLevelAliases = map[string]topLevelAlias{
	"health": {Group: "health", Command: "get"},
	"me":     {Group: "profile", Command: "get"},
	"search": {Group: "search", Command: "run"},
	"whoami": {Group: "profile", Command: "get"},
}

func (v *stringValues) String() string {
	return strings.Join(*v, ",")
}

func (v *stringValues) Set(value string) error {
	*v = append(*v, value)
	return nil
}

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	cli, err := New()
	if err != nil {
		fmt.Fprintf(stderr, "load openapi cli: %v\n", err)
		return 1
	}
	return cli.Run(ctx, args, stdout, stderr)
}

func Routes() []string {
	cli, err := New()
	if err != nil {
		return nil
	}
	return append([]string(nil), cli.routes...)
}

func New() (*CLI, error) {
	swagger, err := api.GetSwagger()
	if err != nil {
		return nil, err
	}

	cli := &CLI{
		groupByName: map[string]*Group{},
	}
	tagDescriptions := tagDescriptionByName(swagger.Tags)

	for _, path := range swagger.Paths.InMatchingOrder() {
		pathItem := swagger.Paths.Value(path)
		for method, operation := range pathItem.Operations() {
			op := buildOperation(path, strings.ToUpper(method), pathItem, operation)
			group := cli.groupByName[op.GroupName]
			if group == nil {
				group = &Group{
					DisplayName: op.GroupDisplayName,
					Name:        op.GroupName,
					Description: strings.TrimSpace(tagDescriptions[op.GroupDisplayName]),
					byName:      map[string]*Operation{},
				}
				cli.groupByName[group.Name] = group
				cli.groups = append(cli.groups, group)
			}
			group.Operations = append(group.Operations, op)
			group.byName[op.Name] = op
			cli.operationCnt++
			cli.routes = append(cli.routes, op.Method+" "+op.Path)
		}
	}

	sort.Slice(cli.groups, func(i, j int) bool {
		return cli.groups[i].Name < cli.groups[j].Name
	})
	for _, group := range cli.groups {
		sort.Slice(group.Operations, func(i, j int) bool {
			if group.Operations[i].Name == group.Operations[j].Name {
				return group.Operations[i].OperationID < group.Operations[j].OperationID
			}
			return group.Operations[i].Name < group.Operations[j].Name
		})
	}
	sort.Strings(cli.routes)

	return cli, nil
}

func (c *CLI) Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	baseURL := strings.TrimSpace(os.Getenv("TERASLACK_BASE_URL"))
	sessionToken := strings.TrimSpace(os.Getenv("TERASLACK_SESSION_TOKEN"))
	apiKey := strings.TrimSpace(os.Getenv("TERASLACK_API_KEY"))
	output := "pretty"

	global := flag.NewFlagSet("teraslack", flag.ContinueOnError)
	global.SetOutput(stderr)
	global.StringVar(&baseURL, "base-url", baseURL, "Teraslack API base URL. Defaults to TERASLACK_BASE_URL or saved config.")
	global.StringVar(&sessionToken, "session-token", sessionToken, "Session token. Defaults to TERASLACK_SESSION_TOKEN or saved config.")
	global.StringVar(&apiKey, "api-key", apiKey, "Bearer API key. Defaults to TERASLACK_API_KEY or saved config.")
	global.StringVar(&output, "output", output, "Output format: pretty or json.")
	global.Usage = func() {
		c.printRootHelp(stderr)
	}
	if err := global.Parse(args); err != nil {
		return 2
	}

	cfg, err := loadFileConfig()
	if err != nil {
		fmt.Fprintf(stderr, "load config: %v\n", err)
		return 1
	}
	baseURL = firstNonEmpty(baseURL, cfg.BaseURL, defaultAuthBaseURL)
	sessionToken = firstNonEmpty(sessionToken, cfg.SessionToken)
	apiKey = firstNonEmpty(apiKey, cfg.APIKey)

	rest := global.Args()
	if len(rest) == 0 {
		c.printRootHelp(stdout)
		return 0
	}

	switch rest[0] {
	case "help":
		return c.runHelp(rest[1:], stdout, stderr)
	case "link":
		return c.runLink(rest[1:], output, stdout, stderr)
	case "routes":
		return c.runRoutes(rest[1:], output, stdout, stderr)
	case "signin":
		return c.runSignIn(ctx, rest[1:], baseURL, stdout, stderr)
	}
	if isLifecycleCommand(rest[0]) {
		return c.runLifecycle(ctx, rest[0], rest[1:], output, stdout, stderr)
	}
	if alias, ok := topLevelAliases[rest[0]]; ok {
		rest = append([]string{alias.Group, alias.Command}, rest[1:]...)
	}

	group := c.groupByName[rest[0]]
	if group == nil {
		fmt.Fprintf(stderr, "unknown group or command %q\n\n", rest[0])
		c.printRootHelp(stderr)
		return 2
	}
	if len(rest) == 1 || rest[1] == "help" {
		c.printGroupHelp(group, stdout)
		return 0
	}

	op := group.byName[rest[1]]
	if op == nil {
		fmt.Fprintf(stderr, "unknown command %q for group %q\n\n", rest[1], group.Name)
		c.printGroupHelp(group, stderr)
		return 2
	}

	return c.runOperation(ctx, op, rest[2:], baseURL, bearerToken(sessionToken, apiKey), output, stdout, stderr)
}

func (c *CLI) runHelp(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		c.printRootHelp(stdout)
		return 0
	}
	if args[0] == "link" {
		c.printLinkHelp(stdout)
		return 0
	}
	if args[0] == "signin" {
		c.printSigninHelp(args[1:], stdout)
		return 0
	}
	if args[0] == "routes" {
		fmt.Fprintln(stdout, "Usage:\n  teraslack routes")
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "List the API routes available in this CLI build.")
		return 0
	}
	if isLifecycleCommand(args[0]) {
		c.printLifecycleHelp(args[0], stdout)
		return 0
	}
	if alias, ok := topLevelAliases[args[0]]; ok {
		group := c.groupByName[alias.Group]
		if group == nil {
			fmt.Fprintf(stderr, "unknown group %q\n", alias.Group)
			return 2
		}
		op := group.byName[alias.Command]
		if op == nil {
			fmt.Fprintf(stderr, "unknown command %q for group %q\n", alias.Command, alias.Group)
			return 2
		}
		c.printOperationHelp(op, stdout)
		return 0
	}

	group := c.groupByName[args[0]]
	if group == nil {
		fmt.Fprintf(stderr, "unknown group %q\n", args[0])
		return 2
	}
	if len(args) == 1 {
		c.printGroupHelp(group, stdout)
		return 0
	}

	op := group.byName[args[1]]
	if op == nil {
		fmt.Fprintf(stderr, "unknown command %q for group %q\n", args[1], group.Name)
		return 2
	}

	c.printOperationHelp(op, stdout)
	return 0
}

func (c *CLI) runRoutes(args []string, output string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("routes", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage:\n  teraslack routes")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(fs.Args()) != 0 {
		fs.Usage()
		return 2
	}

	if output == "json" {
		if err := writeOutput(stdout, c.routes, output); err != nil {
			fmt.Fprintf(stderr, "write output: %v\n", err)
			return 1
		}
		return 0
	}
	for _, route := range c.routes {
		if _, err := fmt.Fprintln(stdout, route); err != nil {
			fmt.Fprintf(stderr, "write output: %v\n", err)
			return 1
		}
	}
	return 0
}

func (c *CLI) runOperation(ctx context.Context, op *Operation, args []string, baseURL, authToken, output string, stdout, stderr io.Writer) int {
	var bodyText string
	var bodyFile string
	var allPages bool
	var setFlags stringValues

	fs := flag.NewFlagSet(op.GroupName+" "+op.Name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&bodyText, "body", "", "JSON request body.")
	fs.StringVar(&bodyFile, "body-file", "", "Path to a JSON request body file.")
	fs.BoolVar(&allPages, "all", false, "Follow pagination until next_cursor is empty when supported.")
	fs.Var(&setFlags, "set", "Set a request body field using key=value. Nested keys may use dot notation.")

	values := map[string]*string{}
	for _, param := range op.Parameters {
		value := ""
		values[param.FlagName] = &value
		fs.StringVar(&value, param.FlagName, "", flagUsage(param))
	}
	bodyValues := map[string]*string{}
	for _, field := range op.BodyFields {
		value := ""
		bodyValues[field.FlagName] = &value
		fs.StringVar(&value, field.FlagName, "", bodyFieldUsage(field))
	}

	fs.Usage = func() {
		c.printOperationHelp(op, stderr)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(baseURL) == "" {
		fmt.Fprintln(stderr, "missing API base URL; pass --base-url, set TERASLACK_BASE_URL, or save it via the CLI config")
		return 2
	}
	if output != "pretty" && output != "json" {
		fmt.Fprintf(stderr, "invalid --output %q, expected pretty or json\n", output)
		return 2
	}
	if op.RequiresAuth && strings.TrimSpace(authToken) == "" {
		fmt.Fprintln(stderr, "missing authentication token; pass --session-token/--api-key, set TERASLACK_SESSION_TOKEN/TERASLACK_API_KEY, or run `teraslack signin ...`")
		return 2
	}

	path, query, body, err := buildRequest(op, values, bodyValues, bodyText, bodyFile, setFlags)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	resp, err := executeOperation(ctx, op, canonicalBaseURL(baseURL), authToken, path, query, body, allPages)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := writeOutput(stdout, resp, output); err != nil {
		fmt.Fprintf(stderr, "write output: %v\n", err)
		return 1
	}
	return 0
}

func buildOperation(path, method string, pathItem *openapi3.PathItem, operation *openapi3.Operation) *Operation {
	groupDisplayName := "Misc"
	if len(operation.Tags) > 0 && strings.TrimSpace(operation.Tags[0]) != "" {
		groupDisplayName = operation.Tags[0]
	}

	op := &Operation{
		GroupDisplayName: groupDisplayName,
		GroupName:        slugify(groupDisplayName),
		Name:             commandName(method, path, groupDisplayName, operation.OperationID),
		OperationID:      operation.OperationID,
		Method:           method,
		Path:             path,
		Summary:          strings.TrimSpace(operation.Summary),
		Description:      strings.TrimSpace(operation.Description),
		RequiresAuth:     requiresAuth(operation),
	}

	for _, paramRef := range mergeParameters(pathItem.Parameters, operation.Parameters) {
		if paramRef == nil || paramRef.Value == nil {
			continue
		}
		param := paramRef.Value
		op.Parameters = append(op.Parameters, Parameter{
			Name:        param.Name,
			FlagName:    param.Name,
			In:          param.In,
			Description: strings.TrimSpace(param.Description),
			Required:    param.Required,
			Schema:      param.Schema,
		})
		if param.In == "query" && param.Name == "cursor" {
			op.CursorField = param.Name
			op.CursorLocation = "query"
		}
	}

	if operation.RequestBody != nil && operation.RequestBody.Value != nil {
		if mediaType, ok := operation.RequestBody.Value.Content["application/json"]; ok && mediaType != nil {
			op.RequestBody = mediaType.Schema
			op.BodyFields = collectBodyFields(mediaType.Schema, op.Parameters)
			if bodyHasCursor(mediaType.Schema) {
				op.CursorField = "cursor"
				op.CursorLocation = "body"
			}
		}
	}

	return op
}

func mergeParameters(pathParams, operationParams openapi3.Parameters) openapi3.Parameters {
	if len(pathParams) == 0 {
		return operationParams
	}
	if len(operationParams) == 0 {
		return pathParams
	}

	merged := make(openapi3.Parameters, 0, len(pathParams)+len(operationParams))
	seen := map[string]int{}
	for _, param := range pathParams {
		if param == nil || param.Value == nil {
			continue
		}
		key := param.Value.In + ":" + param.Value.Name
		seen[key] = len(merged)
		merged = append(merged, param)
	}
	for _, param := range operationParams {
		if param == nil || param.Value == nil {
			continue
		}
		key := param.Value.In + ":" + param.Value.Name
		if idx, ok := seen[key]; ok {
			merged[idx] = param
			continue
		}
		seen[key] = len(merged)
		merged = append(merged, param)
	}
	return merged
}

func buildRequest(op *Operation, values, bodyValues map[string]*string, bodyText, bodyFile string, setFlags []string) (string, map[string]any, any, error) {
	path := op.Path
	query := map[string]any{}
	var body any

	if strings.TrimSpace(bodyText) != "" && strings.TrimSpace(bodyFile) != "" {
		return "", nil, nil, fmt.Errorf("use only one of --body or --body-file")
	}
	if strings.TrimSpace(bodyText) != "" {
		if err := json.Unmarshal([]byte(bodyText), &body); err != nil {
			return "", nil, nil, fmt.Errorf("decode --body: %w", err)
		}
	}
	if strings.TrimSpace(bodyFile) != "" {
		data, err := os.ReadFile(bodyFile)
		if err != nil {
			return "", nil, nil, fmt.Errorf("read --body-file: %w", err)
		}
		if err := json.Unmarshal(data, &body); err != nil {
			return "", nil, nil, fmt.Errorf("decode --body-file: %w", err)
		}
	}

	for _, param := range op.Parameters {
		raw := ""
		if ref := values[param.FlagName]; ref != nil {
			raw = strings.TrimSpace(*ref)
		}
		if raw == "" {
			if param.Required {
				return "", nil, nil, fmt.Errorf("missing required flag --%s", param.FlagName)
			}
			continue
		}

		value, err := convertStringValue(raw, param.Schema)
		if err != nil {
			return "", nil, nil, fmt.Errorf("parse --%s: %w", param.FlagName, err)
		}

		switch param.In {
		case "path":
			path = strings.ReplaceAll(path, "{"+param.Name+"}", url.PathEscape(fmt.Sprint(value)))
		case "query":
			if slice, ok := value.([]any); ok {
				query[param.Name] = joinCSV(slice)
				continue
			}
			query[param.Name] = value
		default:
			return "", nil, nil, fmt.Errorf("unsupported parameter location %q", param.In)
		}
	}

	if len(op.BodyFields) > 0 {
		bodyObject, err := ensureObjectBody(body, bodyValues)
		if err != nil {
			return "", nil, nil, err
		}
		appliedBodyFields := false
		for _, field := range op.BodyFields {
			raw := ""
			if ref := bodyValues[field.FlagName]; ref != nil {
				raw = strings.TrimSpace(*ref)
			}
			if raw == "" {
				continue
			}
			value, err := convertStringValue(raw, field.Schema)
			if err != nil {
				return "", nil, nil, fmt.Errorf("parse --%s: %w", field.FlagName, err)
			}
			applySet(bodyObject, strings.Join(field.Path, "."), value)
			appliedBodyFields = true
		}
		if appliedBodyFields || body != nil {
			body = bodyObject
		}
	}

	if len(setFlags) > 0 {
		objectBody, err := ensureObjectBody(body, map[string]*string{"set": stringPtr("present")})
		if err != nil {
			return "", nil, nil, err
		}
		for _, entry := range setFlags {
			key, value, found := strings.Cut(entry, "=")
			if !found {
				return "", nil, nil, fmt.Errorf("invalid --set %q, expected key=value", entry)
			}
			applySet(objectBody, strings.TrimSpace(key), parseLooseValue(strings.TrimSpace(value)))
		}
		body = objectBody
	}

	if err := validateRequiredBodyFields(body, op.BodyFields); err != nil {
		return "", nil, nil, err
	}

	return path, query, body, nil
}

func executeOperation(ctx context.Context, op *Operation, baseURL, authToken, path string, query map[string]any, body any, allPages bool) (any, error) {
	resp, err := doOperationRequest(ctx, op.Method, baseURL, authToken, path, query, body)
	if err != nil {
		return nil, err
	}
	if !allPages || op.CursorField == "" {
		return resp, nil
	}

	combined, ok := resp.(map[string]any)
	if !ok {
		return resp, nil
	}

	for {
		nextCursor := strings.TrimSpace(stringValue(combined["next_cursor"]))
		if nextCursor == "" {
			delete(combined, "next_cursor")
			return combined, nil
		}

		nextQuery := cloneMap(query)
		nextBody := cloneJSON(body)
		switch op.CursorLocation {
		case "query":
			nextQuery[op.CursorField] = nextCursor
		case "body":
			bodyObject, ok := nextBody.(map[string]any)
			if !ok {
				bodyObject = map[string]any{}
			}
			bodyObject[op.CursorField] = nextCursor
			nextBody = bodyObject
		default:
			return combined, nil
		}

		page, err := doOperationRequest(ctx, op.Method, baseURL, authToken, path, nextQuery, nextBody)
		if err != nil {
			return nil, err
		}
		pageObject, ok := page.(map[string]any)
		if !ok {
			return combined, nil
		}
		mergeCollectionPage(combined, pageObject)
	}
}

func doOperationRequest(ctx context.Context, method, baseURL, authToken, requestPath string, query map[string]any, body any) (any, error) {
	targetURL, err := buildRequestURL(baseURL, requestPath, query)
	if err != nil {
		return nil, fmt.Errorf("build request URL: %w", err)
	}

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, targetURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if strings.TrimSpace(authToken) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(authToken))
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, requestPath, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr api.ErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&apiErr); err == nil && strings.TrimSpace(apiErr.Message) != "" {
			return nil, fmt.Errorf("%s (%s)", apiErr.Message, apiErr.Code)
		}
		bodyBytes, _ := io.ReadAll(resp.Body)
		if len(bodyBytes) > 0 {
			return nil, fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(bodyBytes)))
		}
		return nil, fmt.Errorf("request returned %s", resp.Status)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	if len(strings.TrimSpace(string(bodyBytes))) == 0 {
		return nil, nil
	}

	var decoded any
	if err := json.Unmarshal(bodyBytes, &decoded); err != nil {
		return nil, fmt.Errorf("decode response body: %w", err)
	}
	return decoded, nil
}

func buildRequestURL(baseURL, requestPath string, query map[string]any) (string, error) {
	parsed, err := url.Parse(canonicalBaseURL(baseURL))
	if err != nil {
		return "", err
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + requestPath
	values := parsed.Query()
	for key, value := range query {
		if value == nil {
			continue
		}
		values.Set(key, fmt.Sprint(value))
	}
	parsed.RawQuery = values.Encode()
	return parsed.String(), nil
}

func (c *CLI) printRootHelp(w io.Writer) {
	fmt.Fprintln(w, "Usage:\n  teraslack [global flags] <group> <command> [command flags]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Built-in commands:")
	fmt.Fprintln(w, "  link              Link the current directory to a conversation")
	fmt.Fprintln(w, "  signin            Start an email sign-in and save the session locally")
	fmt.Fprintln(w, "  signout           Clear the saved session token")
	fmt.Fprintln(w, "  me                Show your account and workspace access")
	fmt.Fprintln(w, "  whoami            Show your account and workspace access")
	fmt.Fprintln(w, "  health            Check whether the API is reachable")
	fmt.Fprintln(w, "  search            Search the resources you can access")
	fmt.Fprintln(w, "  routes            List the API routes in this CLI build")
	fmt.Fprintln(w, "  version           Show the installed CLI version")
	fmt.Fprintln(w, "  update            Install the latest published CLI release")
	fmt.Fprintln(w, "  uninstall         Remove the installed CLI")
	fmt.Fprintln(w, "  help              Show help for a command or group")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Global flags:")
	fmt.Fprintln(w, "  --base-url        API base URL to send requests to")
	fmt.Fprintln(w, "  --session-token   Session token to use for authenticated requests")
	fmt.Fprintln(w, "  --api-key         API key to use for authenticated requests")
	fmt.Fprintln(w, "  --output          Output format: pretty or json")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Groups (%d operations):\n", c.operationCnt)
	for _, group := range c.groups {
		description := firstNonEmpty(oneLine(group.Description), group.DisplayName)
		fmt.Fprintf(w, "  %-24s %s\n", group.Name, description)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Use `teraslack help <group>` to list commands in a group.")
}

func (c *CLI) printGroupHelp(group *Group, w io.Writer) {
	fmt.Fprintf(w, "Usage:\n  teraslack [global flags] %s <command> [command flags]\n\n", group.Name)
	fmt.Fprintf(w, "%s commands:\n", group.DisplayName)
	if description := oneLine(group.Description); description != "" {
		fmt.Fprintf(w, "  About: %s\n", description)
	}
	for _, op := range group.Operations {
		summary := firstNonEmpty(op.Summary, op.Description, op.Method+" "+op.Path)
		fmt.Fprintf(w, "  %-28s %s\n", op.Name, oneLine(summary))
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Use `teraslack help %s <command>` for command details.\n", group.Name)
}

func (c *CLI) printOperationHelp(op *Operation, w io.Writer) {
	fmt.Fprintf(w, "Usage:\n  teraslack [global flags] %s %s", op.GroupName, op.Name)
	for _, param := range op.Parameters {
		if param.Required {
			fmt.Fprintf(w, " --%s <%s>", param.FlagName, param.Name)
		}
	}
	if op.RequestBody != nil {
		fmt.Fprint(w, " [--body JSON | --body-file PATH | --set key=value]")
	}
	if op.CursorField != "" {
		fmt.Fprint(w, " [--all]")
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Method: %s\n", op.Method)
	fmt.Fprintf(w, "Path:   %s\n", op.Path)
	if op.Summary != "" {
		fmt.Fprintf(w, "About:  %s\n", oneLine(op.Summary))
	} else if op.Description != "" {
		fmt.Fprintf(w, "About:  %s\n", oneLine(op.Description))
	}
	if op.Description != "" && op.Summary != "" && oneLine(op.Description) != oneLine(op.Summary) {
		fmt.Fprintf(w, "\n%s\n", strings.TrimSpace(op.Description))
	}
	fmt.Fprintln(w)
	if len(op.Parameters) > 0 {
		fmt.Fprintln(w, "Flags:")
		for _, param := range op.Parameters {
			required := ""
			if param.Required {
				required = " Required."
			}
			fmt.Fprintf(w, "  --%-24s %s%s\n", param.FlagName, oneLine(flagUsage(param)), required)
		}
	}
	if len(op.BodyFields) > 0 {
		if len(op.Parameters) == 0 {
			fmt.Fprintln(w, "Flags:")
		}
		for _, field := range op.BodyFields {
			required := ""
			if field.Required {
				required = " Required."
			}
			fmt.Fprintf(w, "  --%-24s %s%s\n", field.FlagName, oneLine(bodyFieldUsage(field)), required)
		}
	}
	if op.RequestBody != nil {
		fmt.Fprintln(w, "  --body                    Send the request body as inline JSON.")
		fmt.Fprintln(w, "  --body-file               Read the request body from a JSON file.")
		fmt.Fprintln(w, "  --set                     Override request body fields with key=value.")
	}
	if op.CursorField != "" {
		fmt.Fprintln(w, "  --all                     Keep fetching pages until next_cursor is empty.")
	}
}

func tagDescriptionByName(tags openapi3.Tags) map[string]string {
	descriptions := make(map[string]string, len(tags))
	for _, tag := range tags {
		if tag == nil {
			continue
		}
		descriptions[strings.TrimSpace(tag.Name)] = strings.TrimSpace(tag.Description)
	}
	return descriptions
}

func flagUsage(param Parameter) string {
	usage := oneLine(param.Description)
	if usage == "" {
		usage = fmt.Sprintf("%s parameter.", param.Name)
	}
	return usage
}

func bodyFieldUsage(field BodyField) string {
	usage := oneLine(field.Description)
	if usage == "" {
		usage = fmt.Sprintf("%s body field.", strings.Join(field.Path, "."))
	}
	return usage
}

func requiresAuth(operation *openapi3.Operation) bool {
	if operation.Security == nil {
		return true
	}
	return len(*operation.Security) != 0
}

func bodyHasCursor(schemaRef *openapi3.SchemaRef) bool {
	schema := derefSchema(schemaRef)
	if schema == nil {
		return false
	}
	_, ok := schema.Properties["cursor"]
	return ok
}

func collectBodyFields(schemaRef *openapi3.SchemaRef, params []Parameter) []BodyField {
	schema := derefSchema(schemaRef)
	if schema == nil || schema.Type == nil || !schema.Type.Is("object") {
		return nil
	}

	var fields []BodyField
	collectBodyFieldsInto(&fields, schemaRef, nil, true)

	reserved := map[string]struct{}{
		"body":      {},
		"body-file": {},
		"set":       {},
		"all":       {},
	}
	for _, param := range params {
		reserved[param.FlagName] = struct{}{}
	}
	for i := range fields {
		base := strings.Join(fields[i].Path, ".")
		if base == "" {
			base = "body"
		}
		name := base
		for suffix := 2; ; suffix++ {
			if _, exists := reserved[name]; !exists {
				break
			}
			name = fmt.Sprintf("%s_field_%d", base, suffix)
		}
		fields[i].FlagName = name
		reserved[name] = struct{}{}
	}

	return fields
}

func collectBodyFieldsInto(fields *[]BodyField, schemaRef *openapi3.SchemaRef, path []string, ancestorsRequired bool) {
	schema := derefSchema(schemaRef)
	if schema == nil {
		return
	}
	if schema.Type == nil || !schema.Type.Is("object") || len(schema.Properties) == 0 {
		if len(path) == 0 {
			return
		}
		*fields = append(*fields, BodyField{
			Path:        append([]string(nil), path...),
			Description: strings.TrimSpace(schema.Description),
			Required:    ancestorsRequired,
			Schema:      schemaRef,
		})
		return
	}

	required := map[string]bool{}
	for _, name := range schema.Required {
		required[name] = true
	}
	names := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		childPath := append(append([]string(nil), path...), name)
		collectBodyFieldsInto(fields, schema.Properties[name], childPath, ancestorsRequired && required[name])
	}
}

var commandNameOverrides = map[string]string{
	"GET /healthz": "get",
}

func commandName(method, path, groupDisplayName, operationID string) string {
	if override := commandNameOverrides[method+" "+path]; override != "" {
		return override
	}
	if strings.TrimSpace(operationID) != "" {
		return commandNameFromOperationID(groupDisplayName, operationID)
	}
	if name := commandNameFromPath(method, path); name != "" {
		return name
	}
	return "call"
}

func commandNameFromPath(method, path string) string {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) == 0 || segments[0] == "" {
		return ""
	}

	switch {
	case len(segments) == 1:
		switch method {
		case "GET":
			return "get"
		case "POST":
			return "create"
		}
	case len(segments) == 2 && isPathParam(segments[1]):
		switch method {
		case "GET":
			return "get"
		case "PATCH", "PUT":
			return "update"
		case "DELETE":
			return "delete"
		}
	case len(segments) == 3 && isPathParam(segments[1]) && !isPathParam(segments[2]):
		switch method {
		case "GET":
			return "list-" + segments[2]
		case "PUT":
			return "set-" + singularizeCommandSegment(segments[2])
		case "POST":
			return "create-" + singularizeCommandSegment(segments[2])
		}
	case len(segments) == 4 && isPathParam(segments[1]) && !isPathParam(segments[2]) && isPathParam(segments[3]):
		switch method {
		case "PATCH":
			return "update-" + singularizeCommandSegment(segments[2])
		case "DELETE":
			return "delete-" + singularizeCommandSegment(segments[2])
		}
	}

	return ""
}

func isPathParam(segment string) bool {
	return strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}")
}

func singularizeCommandSegment(segment string) string {
	switch {
	case strings.HasSuffix(segment, "ies") && len(segment) > 3:
		return strings.TrimSuffix(segment, "ies") + "y"
	case strings.HasSuffix(segment, "s") && len(segment) > 1:
		return strings.TrimSuffix(segment, "s")
	default:
		return segment
	}
}

func commandNameFromOperationID(groupDisplayName, operationID string) string {
	tokens := camelTokens(operationID)
	if len(tokens) == 0 {
		return "call"
	}

	resourceTokens := camelTokens(strings.ReplaceAll(groupDisplayName, " ", ""))
	if len(tokens) > 1 {
		remainder, matched := stripPrefixTokens(tokens[1:], resourceTokens)
		if matched {
			tokens = append(tokens[:1], remainder...)
		}
	}
	return strings.Join(tokens, "-")
}

func stripPrefixTokens(tokens, prefix []string) ([]string, bool) {
	if len(tokens) == 0 || len(prefix) == 0 {
		return tokens, false
	}

	lowered := append([]string(nil), prefix...)
	for i := range lowered {
		lowered[i] = strings.ToLower(lowered[i])
	}

	alternatives := [][]string{lowered}
	if singular := singularTokens(lowered); !equalTokens(singular, lowered) {
		alternatives = append(alternatives, singular)
	}
	if plural := pluralTokens(lowered); !equalTokens(plural, lowered) {
		alternatives = append(alternatives, plural)
	}

	for _, candidate := range alternatives {
		if len(tokens) < len(candidate) {
			continue
		}
		match := true
		for i := range candidate {
			if tokens[i] != candidate[i] {
				match = false
				break
			}
		}
		if match {
			return tokens[len(candidate):], true
		}
	}
	return tokens, false
}

func singularTokens(tokens []string) []string {
	if len(tokens) == 0 {
		return nil
	}
	out := append([]string(nil), tokens...)
	last := out[len(out)-1]
	switch {
	case strings.HasSuffix(last, "ies") && len(last) > 3:
		out[len(out)-1] = strings.TrimSuffix(last, "ies") + "y"
	case strings.HasSuffix(last, "s") && len(last) > 1:
		out[len(out)-1] = strings.TrimSuffix(last, "s")
	}
	return out
}

func pluralTokens(tokens []string) []string {
	if len(tokens) == 0 {
		return nil
	}
	out := append([]string(nil), tokens...)
	last := out[len(out)-1]
	switch {
	case strings.HasSuffix(last, "y") && len(last) > 1:
		out[len(out)-1] = strings.TrimSuffix(last, "y") + "ies"
	case !strings.HasSuffix(last, "s"):
		out[len(out)-1] = last + "s"
	}
	return out
}

func equalTokens(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func camelTokens(input string) []string {
	if input == "" {
		return nil
	}
	input = strings.ReplaceAll(input, "GitHub", "Github")
	input = strings.ReplaceAll(input, "OAuth", "Oauth")
	var tokens []string
	var current []rune
	runes := []rune(input)
	for i, r := range runes {
		if i > 0 && isTokenBoundary(runes[i-1], r, nextRune(runes, i+1)) {
			tokens = append(tokens, strings.ToLower(string(current)))
			current = current[:0]
		}
		if r == '-' || r == '_' || r == ' ' {
			if len(current) > 0 {
				tokens = append(tokens, strings.ToLower(string(current)))
				current = current[:0]
			}
			continue
		}
		current = append(current, r)
	}
	if len(current) > 0 {
		tokens = append(tokens, strings.ToLower(string(current)))
	}
	return tokens
}

func isTokenBoundary(prev, current, next rune) bool {
	if isLower(prev) && isUpper(current) {
		return true
	}
	return isUpper(prev) && isUpper(current) && isLower(next)
}

func isLower(r rune) bool {
	return r >= 'a' && r <= 'z'
}

func isUpper(r rune) bool {
	return r >= 'A' && r <= 'Z'
}

func nextRune(runes []rune, index int) rune {
	if index >= len(runes) {
		return 0
	}
	return runes[index]
}

func slugify(input string) string {
	input = strings.TrimSpace(strings.ToLower(input))
	input = strings.ReplaceAll(input, "_", "-")
	input = strings.ReplaceAll(input, " ", "-")
	for strings.Contains(input, "--") {
		input = strings.ReplaceAll(input, "--", "-")
	}
	return strings.Trim(input, "-")
}

func convertStringValue(raw string, schemaRef *openapi3.SchemaRef) (any, error) {
	schema := derefSchema(schemaRef)
	if schema == nil {
		return raw, nil
	}
	if schema.Nullable && raw == "null" {
		return nil, nil
	}
	if schema.Type == nil {
		return raw, nil
	}

	switch {
	case schema.Type.Is("boolean"):
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, err
		}
		return value, nil
	case schema.Type.Is("integer"):
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, err
		}
		return value, nil
	case schema.Type.Is("number"):
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, err
		}
		return value, nil
	case schema.Type.Is("array"):
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, "[") {
			var value []any
			if err := json.Unmarshal([]byte(trimmed), &value); err == nil {
				return value, nil
			}
		}
		parts := splitCSV(raw)
		items := make([]any, 0, len(parts))
		for _, part := range parts {
			value, err := convertStringValue(part, schema.Items)
			if err != nil {
				return nil, err
			}
			items = append(items, value)
		}
		return items, nil
	case schema.Type.Is("object"):
		if raw == "null" {
			return nil, nil
		}
		var value map[string]any
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			return nil, fmt.Errorf("expected JSON object")
		}
		return value, nil
	default:
		return raw, nil
	}
}

func ensureObjectBody(body any, values map[string]*string) (map[string]any, error) {
	objectBody, ok := body.(map[string]any)
	if body == nil {
		return map[string]any{}, nil
	}
	if ok {
		return objectBody, nil
	}
	for _, ref := range values {
		if ref != nil && strings.TrimSpace(*ref) != "" {
			return nil, fmt.Errorf("body flags require a JSON object body")
		}
	}
	return nil, nil
}

func validateRequiredBodyFields(body any, fields []BodyField) error {
	if len(fields) == 0 {
		return nil
	}
	if body == nil {
		for _, field := range fields {
			if field.Required {
				return fmt.Errorf("missing required flag --%s", field.FlagName)
			}
		}
		return nil
	}
	objectBody, ok := body.(map[string]any)
	if !ok {
		for _, field := range fields {
			if field.Required {
				return fmt.Errorf("request body must be a JSON object")
			}
		}
		return nil
	}
	for _, field := range fields {
		if field.Required && !hasPathValue(objectBody, field.Path) {
			return fmt.Errorf("missing required flag --%s", field.FlagName)
		}
	}
	return nil
}

func hasPathValue(root map[string]any, path []string) bool {
	current := root
	for idx, part := range path {
		value, ok := current[part]
		if !ok {
			return false
		}
		if idx == len(path)-1 {
			return true
		}
		next, ok := value.(map[string]any)
		if !ok {
			return false
		}
		current = next
	}
	return false
}

func stringPtr(value string) *string {
	return &value
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func joinCSV(values []any) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprint(value))
	}
	return strings.Join(parts, ",")
}

func parseLooseValue(raw string) any {
	if raw == "" {
		return ""
	}
	if raw == "null" {
		return nil
	}
	if raw == "true" || raw == "false" {
		value, err := strconv.ParseBool(raw)
		if err == nil {
			return value
		}
	}
	if intValue, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return intValue
	}
	if floatValue, err := strconv.ParseFloat(raw, 64); err == nil && strings.Contains(raw, ".") {
		return floatValue
	}
	if strings.HasPrefix(raw, "{") || strings.HasPrefix(raw, "[") {
		var value any
		if err := json.Unmarshal([]byte(raw), &value); err == nil {
			return value
		}
	}
	return raw
}

func applySet(root map[string]any, key string, value any) {
	parts := strings.Split(strings.TrimSpace(key), ".")
	current := root
	for idx, part := range parts {
		if idx == len(parts)-1 {
			current[part] = value
			return
		}
		next, ok := current[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[part] = next
		}
		current = next
	}
}

func mergeCollectionPage(combined, page map[string]any) {
	combinedItems, _ := combined["items"].([]any)
	pageItems, _ := page["items"].([]any)
	if len(pageItems) > 0 {
		combined["items"] = append(combinedItems, pageItems...)
	}
	if nextCursor, ok := page["next_cursor"]; ok {
		combined["next_cursor"] = nextCursor
	} else {
		delete(combined, "next_cursor")
	}
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func cloneJSON(value any) any {
	if value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var cloned any
	if err := json.Unmarshal(data, &cloned); err != nil {
		return value
	}
	return cloned
}

func derefSchema(ref *openapi3.SchemaRef) *openapi3.Schema {
	if ref == nil {
		return nil
	}
	return ref.Value
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprint(value)
	}
}

func oneLine(input string) string {
	input = strings.TrimSpace(input)
	input = strings.ReplaceAll(input, "\n", " ")
	return strings.Join(strings.Fields(input), " ")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func canonicalBaseURL(baseURL string) string {
	return strings.TrimRight(firstNonEmpty(baseURL, defaultAuthBaseURL), "/")
}

func bearerToken(sessionToken string, apiKey string) string {
	return firstNonEmpty(sessionToken, apiKey)
}

func writeOutput(w io.Writer, value any, output string) error {
	if value == nil {
		_, err := fmt.Fprintln(w, "ok")
		return err
	}
	indent := "  "
	if output == "json" {
		indent = ""
	}
	data, err := marshalOutput(value, indent)
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	_, err = fmt.Fprintln(w)
	return err
}

func marshalOutput(value any, indent string) ([]byte, error) {
	if indent == "" {
		return json.Marshal(value)
	}
	return json.MarshalIndent(value, "", indent)
}

func doJSONRequest(ctx context.Context, method string, targetURL string, body any, authToken string, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, targetURL, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if strings.TrimSpace(authToken) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(authToken))
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr api.ErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&apiErr); err == nil && strings.TrimSpace(apiErr.Message) != "" {
			return fmt.Errorf("%s (%s)", apiErr.Message, apiErr.Code)
		}
		bodyBytes, _ := io.ReadAll(resp.Body)
		if len(bodyBytes) > 0 {
			return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(bodyBytes)))
		}
		return fmt.Errorf("request returned %s", resp.Status)
	}

	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
