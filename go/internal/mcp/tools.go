package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kristofer/codegraph/internal/graph"
	"github.com/kristofer/codegraph/internal/types"
)

// maxOutputLength is the maximum number of characters in a single tool result.
const maxOutputLength = 15000

// ToolDefinition describes an MCP tool.
type ToolDefinition struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"inputSchema"`
}

// InputSchema is the JSON-Schema object for a tool's inputs.
type InputSchema struct {
	Type       string                    `json:"type"`
	Properties map[string]PropertySchema `json:"properties"`
	Required   []string                  `json:"required,omitempty"`
}

// PropertySchema describes a single tool input property.
type PropertySchema struct {
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Enum        []string `json:"enum,omitempty"`
}

// ToolResult is the result returned to the MCP client for a tool call.
type ToolResult struct {
	Content []ContentItem `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ContentItem is a single content item (type + text).
type ContentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// projectPathProp is the common projectPath property shared by all tools.
var projectPathProp = PropertySchema{
	Type:        "string",
	Description: "Path to a different project with .codegraph/ initialized. If omitted, uses current project. Use this to query other codebases.",
}

// toolDefs are the static definitions for all 9 CodeGraph MCP tools.
var toolDefs = []ToolDefinition{
	{
		Name:        "codegraph_search",
		Description: "Quick symbol search by name. Returns locations only (no code). Use codegraph_context instead for comprehensive task context.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"query": {Type: "string", Description: "Symbol name or partial name (e.g., \"auth\", \"signIn\", \"UserService\")"},
				"kind":  {Type: "string", Description: "Filter by node kind", Enum: []string{"function", "method", "class", "interface", "type", "variable", "route", "component"}},
				"limit": {Type: "number", Description: "Maximum results (default: 10)"},
				"projectPath": projectPathProp,
			},
			Required: []string{"query"},
		},
	},
	{
		Name:        "codegraph_context",
		Description: "PRIMARY TOOL: Build comprehensive context for a task. Returns entry points, related symbols, and key code - often enough to understand the codebase without additional tool calls. NOTE: This provides CODE context, not product requirements. For new features, still clarify UX/behavior questions with the user before implementing.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"task":        {Type: "string", Description: "Description of the task, bug, or feature to build context for"},
				"maxNodes":    {Type: "number", Description: "Maximum symbols to include (default: 20)"},
				"includeCode": {Type: "boolean", Description: "Include code snippets for key symbols (default: true)"},
				"projectPath": projectPathProp,
			},
			Required: []string{"task"},
		},
	},
	{
		Name:        "codegraph_callers",
		Description: "Find all functions/methods that call a specific symbol. Useful for understanding usage patterns and impact of changes.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"symbol":      {Type: "string", Description: "Name of the function, method, or class to find callers for"},
				"limit":       {Type: "number", Description: "Maximum number of callers to return (default: 20)"},
				"projectPath": projectPathProp,
			},
			Required: []string{"symbol"},
		},
	},
	{
		Name:        "codegraph_callees",
		Description: "Find all functions/methods that a specific symbol calls. Useful for understanding dependencies and code flow.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"symbol":      {Type: "string", Description: "Name of the function, method, or class to find callees for"},
				"limit":       {Type: "number", Description: "Maximum number of callees to return (default: 20)"},
				"projectPath": projectPathProp,
			},
			Required: []string{"symbol"},
		},
	},
	{
		Name:        "codegraph_impact",
		Description: "Analyze the impact radius of changing a symbol. Shows what code could be affected by modifications.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"symbol":      {Type: "string", Description: "Name of the symbol to analyze impact for"},
				"depth":       {Type: "number", Description: "How many levels of dependencies to traverse (default: 2)"},
				"projectPath": projectPathProp,
			},
			Required: []string{"symbol"},
		},
	},
	{
		Name:        "codegraph_node",
		Description: "Get detailed information about a specific code symbol. Use includeCode=true only when you need the full source code - otherwise just get location and signature to minimize context usage.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"symbol":      {Type: "string", Description: "Name of the symbol to get details for"},
				"includeCode": {Type: "boolean", Description: "Include full source code (default: false to minimize context)"},
				"projectPath": projectPathProp,
			},
			Required: []string{"symbol"},
		},
	},
	{
		Name:        "codegraph_explore",
		Description: "Deep exploration tool — returns comprehensive context for a topic in a SINGLE call. Groups all relevant source code by file (contiguous sections, not snippets), includes a relationship map, and uses deeper graph traversal. Designed to replace multiple codegraph_node + file Read calls. Use this instead of codegraph_context when you need thorough understanding. IMPORTANT: Use specific symbol names, file names, or short code terms in your query — NOT natural language sentences. Before calling this, use codegraph_search to discover relevant symbol names, then include those names in your query. Bad: \"how are agent prompts loaded and passed to the CLI\". Good: \"readAgentsFromDirectory createClaudeSession chat-manager agents.ts\".",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"query":       {Type: "string", Description: "Symbol names, file names, or short code terms to explore (e.g., \"AuthService loginUser session-manager\", \"GraphTraverser BFS impact traversal.ts\"). Use codegraph_search first to find relevant names."},
				"maxFiles":    {Type: "number", Description: "Maximum number of files to include source code from (default: 12)"},
				"projectPath": projectPathProp,
			},
			Required: []string{"query"},
		},
	},
	{
		Name:        "codegraph_status",
		Description: "Get the status of the CodeGraph index, including statistics about indexed files, nodes, and edges.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"projectPath": projectPathProp,
			},
		},
	},
	{
		Name:        "codegraph_files",
		Description: "REQUIRED for file/folder exploration. Get the project file structure from the CodeGraph index. Returns a tree view of all indexed files with metadata (language, symbol count). Much faster than Glob/filesystem scanning. Use this FIRST when exploring project structure, finding files, or understanding codebase organization.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"path":            {Type: "string", Description: "Filter to files under this directory path (e.g., \"src/components\"). Returns all files if not specified."},
				"pattern":         {Type: "string", Description: "Filter files matching this glob pattern (e.g., \"*.tsx\", \"**/*.test.ts\")"},
				"format":          {Type: "string", Description: "Output format: \"tree\" (hierarchical, default), \"flat\" (simple list), \"grouped\" (by language)", Enum: []string{"tree", "flat", "grouped"}},
				"includeMetadata": {Type: "boolean", Description: "Include file metadata like language and symbol count (default: true)"},
				"maxDepth":        {Type: "number", Description: "Maximum directory depth to show (default: unlimited)"},
				"projectPath":     projectPathProp,
			},
		},
	},
}

// getExploreBudget returns the recommended number of explore calls for a project.
func getExploreBudget(fileCount int) int {
	switch {
	case fileCount < 500:
		return 1
	case fileCount < 5000:
		return 2
	case fileCount < 15000:
		return 3
	case fileCount < 25000:
		return 4
	default:
		return 5
	}
}

// clampInt clamps v to [lo, hi].
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// textResult wraps a plain text string into a ToolResult.
func textResult(text string) ToolResult {
	return ToolResult{Content: []ContentItem{{Type: "text", Text: text}}}
}

// errorResult wraps an error message into a ToolResult with IsError=true.
func errorResult(msg string) ToolResult {
	return ToolResult{Content: []ContentItem{{Type: "text", Text: msg}}, IsError: true}
}

// truncateOutput trims output to maxOutputLength, adding a note if truncated.
func truncateOutput(s string) string {
	if len(s) <= maxOutputLength {
		return s
	}
	return s[:maxOutputLength] + "\n\n[Output truncated]"
}

// getTools returns tool definitions, optionally updating the explore description with a budget.
func getTools(p *Project) []ToolDefinition {
	if p == nil {
		return toolDefs
	}
	stats, err := p.queries.GetStats()
	if err != nil {
		return toolDefs
	}
	budget := getExploreBudget(stats.FileCount)
	out := make([]ToolDefinition, len(toolDefs))
	copy(out, toolDefs)
	for i, t := range out {
		if t.Name == "codegraph_explore" {
			out[i].Description = fmt.Sprintf(
				"%s Budget: make at most %d calls for this project (%d files indexed).",
				t.Description, budget, stats.FileCount,
			)
		}
	}
	return out
}

// getCode reads the source lines for a node, validating the path is within projectRoot.
func getCode(projectRoot string, node *types.Node) (string, error) {
	absPath := filepath.Join(projectRoot, node.FilePath)
	clean := filepath.Clean(absPath)
	root := filepath.Clean(projectRoot)
	if !strings.HasPrefix(clean+string(filepath.Separator), root+string(filepath.Separator)) {
		return "", fmt.Errorf("path outside project root")
	}
	content, err := os.ReadFile(clean)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(content), "\n")
	startIdx := clampInt(node.StartLine-1, 0, len(lines))
	endIdx := clampInt(node.EndLine, 0, len(lines))
	return strings.Join(lines[startIdx:endIdx], "\n"), nil
}

// findSymbols searches for all nodes matching the given name.
func findSymbols(p *Project, name string) ([]*types.Node, error) {
	results, err := p.queries.SearchNodes(name, types.SearchOptions{Limit: 50})
	if err != nil {
		return nil, err
	}
	var nodes []*types.Node
	for _, r := range results {
		nodes = append(nodes, r.Node)
	}
	return nodes, nil
}

// formatNode formats a single node as a markdown line.
func formatNode(n *types.Node) string {
	sig := ""
	if n.Signature != nil {
		sig = " — " + *n.Signature
	}
	return fmt.Sprintf("- **%s** (%s) `%s:%d`%s", n.Name, n.Kind, n.FilePath, n.StartLine, sig)
}

// formatNodeList formats a slice of nodes as markdown.
func formatNodeList(nodes []*types.Node, title string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## %s\n\n", title))
	for _, n := range nodes {
		sb.WriteString(formatNode(n))
		sb.WriteByte('\n')
	}
	return sb.String()
}

// formatSearchResults formats search results as markdown.
func formatSearchResults(results []*types.SearchResult) string {
	var sb strings.Builder
	sb.WriteString("## Search Results\n\n")
	for _, r := range results {
		n := r.Node
		sig := ""
		if n.Signature != nil {
			sig = " — " + *n.Signature
		}
		sb.WriteString(fmt.Sprintf("- **%s** (%s) `%s:%d`%s\n", n.Name, n.Kind, n.FilePath, n.StartLine, sig))
	}
	return sb.String()
}

// executeSearch handles the codegraph_search tool.
func executeSearch(p *Project, args map[string]interface{}) ToolResult {
	query, _ := args["query"].(string)
	if query == "" {
		return errorResult("query must be a non-empty string")
	}
	limit := 10
	if v, ok := args["limit"]; ok {
		switch n := v.(type) {
		case float64:
			limit = int(n)
		case int:
			limit = n
		}
	}
	limit = clampInt(limit, 1, 100)

	opts := types.SearchOptions{Limit: limit}
	if kind, ok := args["kind"].(string); ok && kind != "" {
		if k, err := types.ParseNodeKind(kind); err == nil {
			opts.Kinds = []types.NodeKind{k}
		}
	}

	results, err := p.queries.SearchNodes(query, opts)
	if err != nil {
		return errorResult(fmt.Sprintf("search failed: %v", err))
	}
	if len(results) == 0 {
		return textResult(fmt.Sprintf("No results found for %q", query))
	}
	return textResult(truncateOutput(formatSearchResults(results)))
}

// executeContext handles the codegraph_context tool (simplified).
func executeContext(p *Project, args map[string]interface{}) ToolResult {
	task, _ := args["task"].(string)
	if task == "" {
		return errorResult("task must be a non-empty string")
	}
	maxNodes := 20
	if v, ok := args["maxNodes"]; ok {
		if n, ok := v.(float64); ok {
			maxNodes = int(n)
		}
	}
	maxNodes = clampInt(maxNodes, 1, 100)
	includeCode := true
	if v, ok := args["includeCode"]; ok {
		if b, ok := v.(bool); ok {
			includeCode = b
		}
	}

	results, err := p.queries.SearchNodes(task, types.SearchOptions{Limit: maxNodes})
	if err != nil {
		return errorResult(fmt.Sprintf("context search failed: %v", err))
	}
	if len(results) == 0 {
		return textResult(fmt.Sprintf("No relevant symbols found for task: %q", task))
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Context for: %s\n\n", task))
	sb.WriteString("### Relevant Symbols\n\n")

	for _, r := range results {
		n := r.Node
		sb.WriteString(formatNode(n))
		sb.WriteByte('\n')
		if includeCode && n.FilePath != "" {
			code, err := getCode(p.rootDir, n)
			if err == nil && code != "" {
				lang := string(n.Language)
				sb.WriteString(fmt.Sprintf("\n```%s\n%s\n```\n", lang, code))
			}
		}
	}
	return textResult(truncateOutput(sb.String()))
}

// executeCallers handles the codegraph_callers tool.
func executeCallers(p *Project, args map[string]interface{}) ToolResult {
	symbol, _ := args["symbol"].(string)
	if symbol == "" {
		return errorResult("symbol must be a non-empty string")
	}
	limit := 20
	if v, ok := args["limit"]; ok {
		if n, ok := v.(float64); ok {
			limit = int(n)
		}
	}
	limit = clampInt(limit, 1, 100)

	nodes, err := findSymbols(p, symbol)
	if err != nil {
		return errorResult(fmt.Sprintf("symbol lookup failed: %v", err))
	}
	if len(nodes) == 0 {
		return textResult(fmt.Sprintf("Symbol %q not found in the codebase", symbol))
	}

	seen := make(map[string]bool)
	var callers []*types.Node
	traverser := graph.NewGraphTraverser(p.queries)
	for _, n := range nodes {
		results, err := traverser.GetCallers(n.ID, 1)
		if err != nil {
			continue
		}
		for _, c := range results {
			if !seen[c.Node.ID] {
				seen[c.Node.ID] = true
				callers = append(callers, c.Node)
			}
		}
	}

	if len(callers) == 0 {
		return textResult(fmt.Sprintf("No callers found for %q", symbol))
	}
	if len(callers) > limit {
		callers = callers[:limit]
	}
	return textResult(truncateOutput(formatNodeList(callers, "Callers of "+symbol)))
}

// executeCallees handles the codegraph_callees tool.
func executeCallees(p *Project, args map[string]interface{}) ToolResult {
	symbol, _ := args["symbol"].(string)
	if symbol == "" {
		return errorResult("symbol must be a non-empty string")
	}
	limit := 20
	if v, ok := args["limit"]; ok {
		if n, ok := v.(float64); ok {
			limit = int(n)
		}
	}
	limit = clampInt(limit, 1, 100)

	nodes, err := findSymbols(p, symbol)
	if err != nil {
		return errorResult(fmt.Sprintf("symbol lookup failed: %v", err))
	}
	if len(nodes) == 0 {
		return textResult(fmt.Sprintf("Symbol %q not found in the codebase", symbol))
	}

	seen := make(map[string]bool)
	var callees []*types.Node
	traverser := graph.NewGraphTraverser(p.queries)
	for _, n := range nodes {
		results, err := traverser.GetCallees(n.ID, 1)
		if err != nil {
			continue
		}
		for _, c := range results {
			if !seen[c.Node.ID] {
				seen[c.Node.ID] = true
				callees = append(callees, c.Node)
			}
		}
	}

	if len(callees) == 0 {
		return textResult(fmt.Sprintf("No callees found for %q", symbol))
	}
	if len(callees) > limit {
		callees = callees[:limit]
	}
	return textResult(truncateOutput(formatNodeList(callees, "Callees of "+symbol)))
}

// executeImpact handles the codegraph_impact tool.
func executeImpact(p *Project, args map[string]interface{}) ToolResult {
	symbol, _ := args["symbol"].(string)
	if symbol == "" {
		return errorResult("symbol must be a non-empty string")
	}
	depth := 2
	if v, ok := args["depth"]; ok {
		if n, ok := v.(float64); ok {
			depth = int(n)
		}
	}
	depth = clampInt(depth, 1, 10)

	nodes, err := findSymbols(p, symbol)
	if err != nil {
		return errorResult(fmt.Sprintf("symbol lookup failed: %v", err))
	}
	if len(nodes) == 0 {
		return textResult(fmt.Sprintf("Symbol %q not found in the codebase", symbol))
	}

	traverser := graph.NewGraphTraverser(p.queries)
	mergedNodes := make(map[string]*types.Node)
	seen := make(map[string]bool)
	var mergedEdges []*types.Edge

	for _, n := range nodes {
		impact, err := traverser.GetImpactRadius(n.ID, depth)
		if err != nil {
			continue
		}
		for id, node := range impact.Nodes {
			mergedNodes[id] = node
		}
		for _, e := range impact.Edges {
			key := e.Source + "->" + e.Target + ":" + string(e.Kind)
			if !seen[key] {
				seen[key] = true
				mergedEdges = append(mergedEdges, e)
			}
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Impact Radius: %s\n\n", symbol))
	sb.WriteString(fmt.Sprintf("Found %d affected symbols across %d edges.\n\n", len(mergedNodes), len(mergedEdges)))

	// Sort nodes by file for readability
	var nodeList []*types.Node
	for _, n := range mergedNodes {
		nodeList = append(nodeList, n)
	}
	sort.Slice(nodeList, func(i, j int) bool {
		if nodeList[i].FilePath != nodeList[j].FilePath {
			return nodeList[i].FilePath < nodeList[j].FilePath
		}
		return nodeList[i].StartLine < nodeList[j].StartLine
	})

	sb.WriteString("### Affected Symbols\n\n")
	for _, n := range nodeList {
		sb.WriteString(formatNode(n))
		sb.WriteByte('\n')
	}

	if len(mergedEdges) > 0 {
		sb.WriteString("\n### Relationships\n\n")
		shown := mergedEdges
		if len(shown) > 30 {
			shown = shown[:30]
		}
		for _, e := range shown {
			src := mergedNodes[e.Source]
			tgt := mergedNodes[e.Target]
			srcName, tgtName := e.Source, e.Target
			if src != nil {
				srcName = src.Name
			}
			if tgt != nil {
				tgtName = tgt.Name
			}
			sb.WriteString(fmt.Sprintf("- %s **%s** %s\n", srcName, e.Kind, tgtName))
		}
		if len(mergedEdges) > 30 {
			sb.WriteString(fmt.Sprintf("- ... and %d more\n", len(mergedEdges)-30))
		}
	}
	return textResult(truncateOutput(sb.String()))
}

// executeNode handles the codegraph_node tool.
func executeNode(p *Project, args map[string]interface{}) ToolResult {
	symbol, _ := args["symbol"].(string)
	if symbol == "" {
		return errorResult("symbol must be a non-empty string")
	}
	includeCode := false
	if v, ok := args["includeCode"]; ok {
		if b, ok := v.(bool); ok {
			includeCode = b
		}
	}

	nodes, err := findSymbols(p, symbol)
	if err != nil {
		return errorResult(fmt.Sprintf("symbol lookup failed: %v", err))
	}
	if len(nodes) == 0 {
		return textResult(fmt.Sprintf("Symbol %q not found in the codebase", symbol))
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Symbol: %s\n\n", symbol))
	for _, n := range nodes {
		sb.WriteString(fmt.Sprintf("### %s (%s)\n\n", n.Name, n.Kind))
		sb.WriteString(fmt.Sprintf("- **File:** `%s:%d-%d`\n", n.FilePath, n.StartLine, n.EndLine))
		sb.WriteString(fmt.Sprintf("- **Language:** %s\n", n.Language))
		if n.Signature != nil {
			sb.WriteString(fmt.Sprintf("- **Signature:** `%s`\n", *n.Signature))
		}
		if n.Visibility != nil {
			sb.WriteString(fmt.Sprintf("- **Visibility:** %s\n", *n.Visibility))
		}
		if n.Docstring != nil && *n.Docstring != "" {
			sb.WriteString(fmt.Sprintf("- **Docs:** %s\n", *n.Docstring))
		}
		if includeCode {
			code, err := getCode(p.rootDir, n)
			if err == nil && code != "" {
				sb.WriteString(fmt.Sprintf("\n```%s\n%s\n```\n", n.Language, code))
			}
		}
		sb.WriteByte('\n')
	}
	return textResult(truncateOutput(sb.String()))
}

// executeExplore handles the codegraph_explore tool (simplified).
func executeExplore(p *Project, args map[string]interface{}) ToolResult {
	query, _ := args["query"].(string)
	if query == "" {
		return errorResult("query must be a non-empty string")
	}
	maxFiles := 12
	if v, ok := args["maxFiles"]; ok {
		if n, ok := v.(float64); ok {
			maxFiles = int(n)
		}
	}
	maxFiles = clampInt(maxFiles, 1, 20)

	// Search for relevant symbols using query terms
	results, err := p.queries.SearchNodes(query, types.SearchOptions{Limit: 50})
	if err != nil {
		return errorResult(fmt.Sprintf("explore search failed: %v", err))
	}
	if len(results) == 0 {
		return textResult(fmt.Sprintf("No relevant code found for %q", query))
	}

	// Group nodes by file
	type fileGroup struct {
		nodes []*types.Node
		score float64
	}
	fileGroups := make(map[string]*fileGroup)
	for _, r := range results {
		n := r.Node
		if n.Kind == types.NodeKindImport || n.Kind == types.NodeKindExport {
			continue
		}
		fg := fileGroups[n.FilePath]
		if fg == nil {
			fg = &fileGroup{}
			fileGroups[n.FilePath] = fg
		}
		fg.nodes = append(fg.nodes, n)
		fg.score += r.Score
	}

	// Sort files by score descending
	type filePair struct {
		path string
		fg   *fileGroup
	}
	var sorted []filePair
	for path, fg := range fileGroups {
		sorted = append(sorted, filePair{path, fg})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].fg.score > sorted[j].fg.score
	})

	const exploreMaxOutput = 35000
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Exploration: %s\n\n", query))
	sb.WriteString(fmt.Sprintf("Found %d symbols across %d files.\n\n", len(results), len(fileGroups)))

	sb.WriteString("### Source Code\n\n")
	filesIncluded := 0
	for _, fp := range sorted {
		if filesIncluded >= maxFiles {
			break
		}
		if sb.Len() > int(float64(exploreMaxOutput)*0.9) {
			break
		}

		absPath := filepath.Join(p.rootDir, fp.path)
		clean := filepath.Clean(absPath)
		root := filepath.Clean(p.rootDir)
		if !strings.HasPrefix(clean+string(filepath.Separator), root+string(filepath.Separator)) {
			continue
		}
		content, err := os.ReadFile(clean)
		if err != nil {
			continue
		}

		lines := strings.Split(string(content), "\n")
		nodes := fp.fg.nodes

		// Compute a contiguous section covering all nodes in this file
		minLine, maxLine := nodes[0].StartLine, nodes[0].EndLine
		for _, n := range nodes {
			if n.StartLine < minLine {
				minLine = n.StartLine
			}
			if n.EndLine > maxLine {
				maxLine = n.EndLine
			}
		}
		// Add context lines around the section
		startIdx := clampInt(minLine-3, 0, len(lines))
		endIdx := clampInt(maxLine+2, 0, len(lines))
		section := strings.Join(lines[startIdx:endIdx], "\n")

		// Skip if section is empty
		if strings.TrimSpace(section) == "" {
			continue
		}

		lang := string(nodes[0].Language)
		sb.WriteString(fmt.Sprintf("#### `%s` (lines %d-%d)\n\n", fp.path, startIdx+1, endIdx))
		sb.WriteString(fmt.Sprintf("```%s\n%s\n```\n\n", lang, section))
		filesIncluded++
	}

	out := sb.String()
	if len(out) > exploreMaxOutput {
		out = out[:exploreMaxOutput] + "\n\n[Output truncated]"
	}
	return textResult(out)
}

// executeStatus handles the codegraph_status tool.
func executeStatus(p *Project) ToolResult {
	stats, err := p.queries.GetStats()
	if err != nil {
		return errorResult(fmt.Sprintf("failed to get stats: %v", err))
	}

	var sb strings.Builder
	sb.WriteString("## CodeGraph Status\n\n")
	sb.WriteString(fmt.Sprintf("- **Files indexed:** %d\n", stats.FileCount))
	sb.WriteString(fmt.Sprintf("- **Nodes:** %d\n", stats.NodeCount))
	sb.WriteString(fmt.Sprintf("- **Edges:** %d\n", stats.EdgeCount))
	sb.WriteString(fmt.Sprintf("- **DB size:** %d bytes\n", stats.DBSizeBytes))

	if len(stats.FilesByLanguage) > 0 {
		sb.WriteString("\n### Files by Language\n\n")
		type langCount struct {
			lang  string
			count int
		}
		var langs []langCount
		for l, c := range stats.FilesByLanguage {
			langs = append(langs, langCount{string(l), c})
		}
		sort.Slice(langs, func(i, j int) bool { return langs[i].count > langs[j].count })
		for _, lc := range langs {
			sb.WriteString(fmt.Sprintf("- %s: %d\n", lc.lang, lc.count))
		}
	}

	if len(stats.NodesByKind) > 0 {
		sb.WriteString("\n### Nodes by Kind\n\n")
		type kindCount struct {
			kind  string
			count int
		}
		var kinds []kindCount
		for k, c := range stats.NodesByKind {
			kinds = append(kinds, kindCount{string(k), c})
		}
		sort.Slice(kinds, func(i, j int) bool { return kinds[i].count > kinds[j].count })
		for _, kc := range kinds {
			sb.WriteString(fmt.Sprintf("- %s: %d\n", kc.kind, kc.count))
		}
	}

	return textResult(sb.String())
}

// executeFiles handles the codegraph_files tool.
func executeFiles(p *Project, args map[string]interface{}) ToolResult {
	pathFilter, _ := args["path"].(string)
	pattern, _ := args["pattern"].(string)
	format, _ := args["format"].(string)
	if format == "" {
		format = "tree"
	}
	includeMetadata := true
	if v, ok := args["includeMetadata"]; ok {
		if b, ok := v.(bool); ok {
			includeMetadata = b
		}
	}
	maxDepth := 0 // 0 = unlimited
	if v, ok := args["maxDepth"]; ok {
		if n, ok := v.(float64); ok {
			maxDepth = int(n)
		}
	}

	files, err := p.queries.GetAllFiles()
	if err != nil {
		return errorResult(fmt.Sprintf("failed to get files: %v", err))
	}

	// Filter files
	var filtered []*types.FileRecord
	for _, f := range files {
		if pathFilter != "" && !strings.HasPrefix(f.Path, pathFilter) {
			continue
		}
		if pattern != "" && !matchGlob(pattern, filepath.Base(f.Path)) {
			continue
		}
		filtered = append(filtered, f)
	}

	if len(filtered) == 0 {
		return textResult("No files found matching the criteria.")
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Project Files (%d indexed)\n\n", len(filtered)))

	switch format {
	case "flat":
		for _, f := range filtered {
			if includeMetadata {
				sb.WriteString(fmt.Sprintf("- `%s` (%s, %d symbols)\n", f.Path, f.Language, f.NodeCount))
			} else {
				sb.WriteString(fmt.Sprintf("- `%s`\n", f.Path))
			}
		}
	case "grouped":
		grouped := make(map[string][]*types.FileRecord)
		for _, f := range filtered {
			lang := string(f.Language)
			grouped[lang] = append(grouped[lang], f)
		}
		var langs []string
		for l := range grouped {
			langs = append(langs, l)
		}
		sort.Strings(langs)
		for _, lang := range langs {
			sb.WriteString(fmt.Sprintf("### %s\n\n", lang))
			for _, f := range grouped[lang] {
				if includeMetadata {
					sb.WriteString(fmt.Sprintf("- `%s` (%d symbols)\n", f.Path, f.NodeCount))
				} else {
					sb.WriteString(fmt.Sprintf("- `%s`\n", f.Path))
				}
			}
			sb.WriteByte('\n')
		}
	default: // "tree"
		sb.WriteString(buildFileTree(filtered, includeMetadata, maxDepth))
	}

	return textResult(truncateOutput(sb.String()))
}

// matchGlob is a simple glob matcher for filename patterns.
func matchGlob(pattern, name string) bool {
	matched, err := filepath.Match(pattern, name)
	return err == nil && matched
}

// buildFileTree renders a tree view of files.
func buildFileTree(files []*types.FileRecord, includeMetadata bool, maxDepth int) string {
	type treeNode struct {
		children map[string]*treeNode
		file     *types.FileRecord
	}
	root := &treeNode{children: make(map[string]*treeNode)}

	for _, f := range files {
		parts := strings.Split(filepath.ToSlash(f.Path), "/")
		cur := root
		for i, part := range parts {
			if _, ok := cur.children[part]; !ok {
				cur.children[part] = &treeNode{children: make(map[string]*treeNode)}
			}
			cur = cur.children[part]
			if i == len(parts)-1 {
				cur.file = f
			}
		}
	}

	var sb strings.Builder
	var walk func(node *treeNode, prefix string, depth int)
	walk = func(node *treeNode, prefix string, depth int) {
		if maxDepth > 0 && depth > maxDepth {
			return
		}
		// Sort children: dirs first, then files, alphabetically
		var keys []string
		for k := range node.children {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for i, k := range keys {
			child := node.children[k]
			isLast := i == len(keys)-1
			connector := "├── "
			nextPrefix := prefix + "│   "
			if isLast {
				connector = "└── "
				nextPrefix = prefix + "    "
			}
			if child.file != nil {
				if includeMetadata {
					sb.WriteString(fmt.Sprintf("%s%s`%s` (%s, %d symbols)\n", prefix, connector, k, child.file.Language, child.file.NodeCount))
				} else {
					sb.WriteString(fmt.Sprintf("%s%s`%s`\n", prefix, connector, k))
				}
			} else {
				sb.WriteString(fmt.Sprintf("%s%s**%s/**\n", prefix, connector, k))
				walk(child, nextPrefix, depth+1)
			}
		}
	}
	walk(root, "", 0)
	return sb.String()
}
