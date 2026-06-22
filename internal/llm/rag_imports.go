package llm

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// ────────────────────────────────────────────────────────────────────────────
// Import extraction
// ────────────────────────────────────────────────────────────────────────────

// ImportInfo holds all imports extracted from a single source file.
type ImportInfo struct {
	FilePath  string
	Language  string
	Imports   []string // raw import strings as they appear in source
	Libraries []string // deduplicated third-party/external packages only
}

// ExtractImports parses source code and returns all import statements.
// Language is detected from the file extension.
// Supports: Go, Python, JavaScript/TypeScript, Java, Rust, Ruby, Kotlin, Swift.
func ExtractImports(filePath, content string) ImportInfo {
	ext := strings.ToLower(filepath.Ext(filePath))
	lang, imports := detectAndExtract(ext, content)

	return ImportInfo{
		FilePath:  filePath,
		Language:  lang,
		Imports:   imports,
		Libraries: filterExternal(imports, lang),
	}
}

// ExtractImportsFromFiles runs ExtractImports on multiple files and returns
// a deduplicated aggregated import list — useful for RAG context assembly.
func ExtractImportsFromFiles(files map[string]string) []ImportInfo {
	results := make([]ImportInfo, 0, len(files))
	for path, content := range files {
		info := ExtractImports(path, content)
		if len(info.Imports) > 0 {
			results = append(results, info)
		}
	}
	return results
}

// BuildImportContext formats ImportInfo slices into a concise string suitable
// for injection into the RAG context prompt.
func BuildImportContext(infos []ImportInfo) string {
	if len(infos) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Dependency Context (extracted imports)\n\n")

	for _, info := range infos {
		if len(info.Libraries) == 0 {
			continue
		}
		sb.WriteString(fmt.Sprintf("**%s** (%s)\n", info.FilePath, info.Language))
		for _, lib := range info.Libraries {
			sb.WriteString(fmt.Sprintf("  - `%s`\n", lib))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// ────────────────────────────────────────────────────────────────────────────
// RAG context builder with imports (pipeline integration)
// ────────────────────────────────────────────────────────────────────────────

// BuildContextWithImports extends BuildContext by appending extracted import
// information before sending to the LLM. This gives the model richer context
// about which libraries are involved in the diff.
func BuildContextWithImports(
	ctx context.Context,
	client LLMClient,
	diff string,
	fileContents map[string]string, // filePath → content
) (*ContextResult, error) {
	// 1. Extract imports from all touched files.
	importInfos := ExtractImportsFromFiles(fileContents)
	importContext := BuildImportContext(importInfos)

	// 2. Combine diff + import context as the user prompt.
	userPrompt := fmt.Sprintf(
		"Analyse this code diff and return the JSON summary.\n\n%s\n\nCode Diff:\n%s",
		importContext, diff,
	)

	raw, err := client.Complete(ctx, PromptContextBuilder, userPrompt)
	if err != nil {
		return &ContextResult{ArchitectureSummary: importContext}, nil
	}

	var result ContextResult
	if err := parseJSON(raw, &result); err != nil {
		return &ContextResult{ArchitectureSummary: raw}, nil
	}
	return &result, nil
}

// ────────────────────────────────────────────────────────────────────────────
// Five-optimisation pipeline entry point (used by Pipeline.Run)
// ────────────────────────────────────────────────────────────────────────────

// ParseDiffFiles parses a unified diff and returns a map of
// filePath → concatenated addition lines (+). Only added code is extracted
// because that is the code the LLM needs to analyse for imports.
func ParseDiffFiles(diff string) map[string]string {
	files := make(map[string]string)
	var currentFile string
	var sb strings.Builder

	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+++ b/"):
			// Flush previous file.
			if currentFile != "" && sb.Len() > 0 {
				files[currentFile] = sb.String()
				sb.Reset()
			}
			currentFile = strings.TrimPrefix(line, "+++ b/")
		case currentFile != "" &&
			strings.HasPrefix(line, "+") &&
			!strings.HasPrefix(line, "+++"):
			// Collect added lines (strip the leading '+').
			sb.WriteString(strings.TrimPrefix(line, "+"))
			sb.WriteString("\n")
		}
	}
	if currentFile != "" && sb.Len() > 0 {
		files[currentFile] = sb.String()
	}
	return files
}

// truncateImportPath reduces a deep module path to its base module root
// (at most 3 slash-separated segments, e.g. "github.com/gin-gonic/gin").
// This strips noisy sub-package paths (e.g. ".../gin/internal/render")
// before they reach the LLM — Optimisation 3.
func truncateImportPath(imp string) string {
	parts := strings.Split(imp, "/")
	if len(parts) <= 3 {
		return imp
	}
	return strings.Join(parts[:3], "/")
}

// intersectWithDiff filters libs to only those whose base package name or
// full path appears somewhere in the diff text.
// Every entry in the returned slice is guaranteed to be actively involved in
// the provided diff — Optimisation 5.
func intersectWithDiff(libs []string, diff string) []string {
	var out []string
	for _, lib := range libs {
		// Match on full path OR just the leaf package name.
		leafName := lib[strings.LastIndex(lib, "/")+1:]
		if strings.Contains(diff, lib) || strings.Contains(diff, leafName) {
			out = append(out, lib)
		}
	}
	return out
}

// BuildContextFromDiff is the optimised RAG entry point used by Pipeline.Run.
// It applies all five context optimisations before calling the LLM:
//
//  1. Flattens and deduplicates the import list (no repeated entries).
//  2. Drops internal / local file imports (filterExternal inside ExtractImports).
//  3. Truncates deep sub-package paths to their base module root.
//  4. Operates exclusively on the unified diff — not full file contents.
//  5. Intersects the import list with the diff text: only libraries that
//     appear in the diff are forwarded, so the LLM can rely on the list
//     entirely for framework-specific advice without hallucinating tech stacks.
func BuildContextFromDiff(ctx context.Context, client LLMClient, diff string) (*ContextResult, error) {
	// Steps 1–3: parse additions, extract, filter stdlib, truncate, dedupe.
	diffFiles := ParseDiffFiles(diff)
	importInfos := ExtractImportsFromFiles(diffFiles)

	seen := make(map[string]struct{})
	var flatLibs []string
	for _, info := range importInfos {
		for _, lib := range info.Libraries {
			base := truncateImportPath(lib)
			if _, ok := seen[base]; !ok {
				seen[base] = struct{}{}
				flatLibs = append(flatLibs, base)
			}
		}
	}

	// Step 5: intersect — keep only what the diff actually references.
	activeLibs := intersectWithDiff(flatLibs, diff)

	// Build the slim dependency context block.
	var importCtx string
	if len(activeLibs) > 0 {
		var sb strings.Builder
		sb.WriteString("## Active Dependency Context\n\n")
		for _, lib := range activeLibs {
			sb.WriteString(fmt.Sprintf("- `%s`\n", lib))
		}
		importCtx = sb.String()
	}

	const maxDiffTokens = 8000
	estimatedTokens := len(diff) / 4
	if estimatedTokens > maxDiffTokens {
		diff = SmartDiffPreprocessing(diff, maxDiffTokens)
	}

	userPrompt := fmt.Sprintf(
		"Analyse this code diff and return the JSON summary.\n\n%s\n\nCode Diff:\n%s",
		importCtx, diff,
	)

	raw, err := client.Complete(ctx, PromptContextBuilder, userPrompt)
	if err != nil {
		fmt.Printf("ERROR: LLM Complete failed in BuildContextFromDiff: %v\n", err)
		// Graceful fallback: return the dependency list as the summary.
		return &ContextResult{ArchitectureSummary: importCtx}, nil
	}

	var result ContextResult
	if err := parseJSON(raw, &result); err != nil {
		fmt.Printf("ERROR: LLM returned invalid JSON in BuildContextFromDiff: %v\nRaw response:\n%s\n", err, raw)
		return &ContextResult{ArchitectureSummary: raw}, nil
	}

	if err := result.Validate(); err != nil {
		fmt.Printf("ERROR: LLM JSON failed validation in BuildContextFromDiff: %v\nRaw response:\n%s\n", err, raw)
		return &ContextResult{ArchitectureSummary: raw}, nil
	}

	return &result, nil
}

// SmartDiffPreprocessing intelligently truncates a diff string by keeping full context for risky hunks and summarizing the rest.
func SmartDiffPreprocessing(diff string, maxTokens int) string {
	lines := strings.Split(diff, "\n")
	hunks := ParseHunks(lines)
	
	var output []string
	
	for _, hunk := range hunks {
		if ContainsRiskyOperations(hunk) {
			// KEEP: Full implementation for security-sensitive code
			output = append(output, hunk.FullContent()...)
		} else {
			// KEEP: Only changed lines (diff markers + context)
			output = append(output, hunk.OnlyChangedLines()...)
		}
	}
	
	result := strings.Join(output, "\n")
	
	// If STILL too large, hard truncate
	if len(result)/4 > maxTokens {
		maxChars := maxTokens * 4
		if len(result) > maxChars {
			return result[:maxChars] + "\n... [Diff Truncated due to size limits] ..."
		}
	}
	
	return result
}

// Hunk represents a unified diff hunk
type Hunk struct {
	Header     string
	Lines      []DiffLine
	FuncName   string
}

// DiffLine represents a single line in a diff
type DiffLine struct {
	Raw string
}

func (l DiffLine) IsChanged() bool {
	return strings.HasPrefix(l.Raw, "+") || strings.HasPrefix(l.Raw, "-")
}

func (h Hunk) OnlyChangedLines() []string {
	var output []string
	if h.FuncName != "" {
		output = append(output, "// "+h.FuncName)
	}
	output = append(output, h.Header)
	
	for _, line := range h.Lines {
		if line.IsChanged() && !strings.HasPrefix(line.Raw, "+++") && !strings.HasPrefix(line.Raw, "---") {
			output = append(output, line.Raw)
		}
	}
	return output
}

func (h Hunk) FullContent() []string {
	var output []string
	output = append(output, h.Header)
	for _, line := range h.Lines {
		output = append(output, line.Raw)
	}
	return output
}

func ParseHunks(lines []string) []Hunk {
	var hunks []Hunk
	var currentHunk *Hunk

	for _, line := range lines {
		if strings.HasPrefix(line, "@@ ") {
			if currentHunk != nil {
				hunks = append(hunks, *currentHunk)
			}
			currentHunk = &Hunk{Header: line}
			
			// Attempt to extract function name
			parts := strings.SplitN(line, "@@", 3)
			if len(parts) == 3 {
				currentHunk.FuncName = strings.TrimSpace(parts[2])
			}
		} else if currentHunk != nil {
			currentHunk.Lines = append(currentHunk.Lines, DiffLine{Raw: line})
		}
	}
	if currentHunk != nil {
		hunks = append(hunks, *currentHunk)
	}
	return hunks
}

func ContainsRiskyOperations(hunk Hunk) bool {
	content := strings.ToLower(strings.Join(hunk.FullContent(), "\n"))
	riskPatterns := map[string][]string{
		"cryptography": {"encrypt", "decrypt", "cipher", "key", "hash", "signing"},
		"sql":          {"select", "insert", "update", "delete", "execute", "prepare"},
		"auth":         {"password", "token", "permission", "role", "admin", "access"},
		"data":         {"truncate", "drop", "delete where", "cascade"},
		"network":      {"exec", "system", "shell", "command"},
	}
	
	for _, keywords := range riskPatterns {
		for _, kw := range keywords {
			if strings.Contains(content, kw) {
				return true
			}
		}
	}
	return false
}

// ────────────────────────────────────────────────────────────────────────────
// Language-specific extractors
// ────────────────────────────────────────────────────────────────────────────

func detectAndExtract(ext, content string) (string, []string) {
	switch ext {
	case ".go":
		return "Go", extractGo(content)
	case ".py":
		return "Python", extractPython(content)
	case ".js", ".mjs", ".cjs":
		return "JavaScript", extractJS(content)
	case ".ts", ".tsx":
		return "TypeScript", extractJS(content)
	case ".java":
		return "Java", extractJava(content)
	case ".rs":
		return "Rust", extractRust(content)
	case ".rb":
		return "Ruby", extractRuby(content)
	case ".kt", ".kts":
		return "Kotlin", extractJava(content) // Kotlin uses same import syntax
	case ".swift":
		return "Swift", extractSwift(content)
	default:
		return "unknown", nil
	}
}

// Go: import "pkg" or import ( "pkg" )
var (
	goImportSingle = regexp.MustCompile(`(?m)^import\s+"([^"]+)"`)
	goImportBlock  = regexp.MustCompile(`"([^"]+)"`)
	goImportStart  = regexp.MustCompile(`(?ms)import\s*\((.+?)\)`)
)

func extractGo(content string) []string {
	var imports []string
	// Single-line imports
	for _, m := range goImportSingle.FindAllStringSubmatch(content, -1) {
		imports = append(imports, m[1])
	}
	// Block imports
	for _, block := range goImportStart.FindAllStringSubmatch(content, -1) {
		for _, m := range goImportBlock.FindAllStringSubmatch(block[1], -1) {
			imports = append(imports, m[1])
		}
	}
	return dedupe(imports)
}

// Python: import x, from x import y
var (
	pyImport     = regexp.MustCompile(`(?m)^import\s+([\w.]+)`)
	pyFromImport = regexp.MustCompile(`(?m)^from\s+([\w.]+)\s+import`)
)

func extractPython(content string) []string {
	var imports []string
	for _, m := range pyImport.FindAllStringSubmatch(content, -1) {
		imports = append(imports, m[1])
	}
	for _, m := range pyFromImport.FindAllStringSubmatch(content, -1) {
		imports = append(imports, m[1])
	}
	return dedupe(imports)
}

// JS/TS: import ... from "pkg" or require("pkg")
var (
	jsImport  = regexp.MustCompile(`(?m)import\s+.+?\s+from\s+['"]([^'"]+)['"]`)
	jsRequire = regexp.MustCompile(`require\(['"]([^'"]+)['"]\)`)
)

func extractJS(content string) []string {
	var imports []string
	for _, m := range jsImport.FindAllStringSubmatch(content, -1) {
		imports = append(imports, m[1])
	}
	for _, m := range jsRequire.FindAllStringSubmatch(content, -1) {
		imports = append(imports, m[1])
	}
	return dedupe(imports)
}

// Java/Kotlin: import com.example.Foo;
var javaImport = regexp.MustCompile(`(?m)^import\s+([\w.]+);`)

func extractJava(content string) []string {
	var imports []string
	for _, m := range javaImport.FindAllStringSubmatch(content, -1) {
		imports = append(imports, m[1])
	}
	return dedupe(imports)
}

// Rust: use crate::x; or extern crate x;
var (
	rustUse    = regexp.MustCompile(`(?m)^use\s+([\w:]+)`)
	rustExtern = regexp.MustCompile(`(?m)^extern\s+crate\s+(\w+)`)
)

func extractRust(content string) []string {
	var imports []string
	for _, m := range rustUse.FindAllStringSubmatch(content, -1) {
		imports = append(imports, m[1])
	}
	for _, m := range rustExtern.FindAllStringSubmatch(content, -1) {
		imports = append(imports, m[1])
	}
	return dedupe(imports)
}

// Ruby: require "gem" or require_relative "file"
var rubyRequire = regexp.MustCompile(`(?m)^require\s+['"]([^'"]+)['"]`)

func extractRuby(content string) []string {
	var imports []string
	for _, m := range rubyRequire.FindAllStringSubmatch(content, -1) {
		imports = append(imports, m[1])
	}
	return dedupe(imports)
}

// Swift: import Module
var swiftImport = regexp.MustCompile(`(?m)^import\s+(\w+)`)

func extractSwift(content string) []string {
	var imports []string
	for _, m := range swiftImport.FindAllStringSubmatch(content, -1) {
		imports = append(imports, m[1])
	}
	return dedupe(imports)
}

// ────────────────────────────────────────────────────────────────────────────
// Filters & helpers
// ────────────────────────────────────────────────────────────────────────────

// stdlibPrefixes contains well-known stdlib / language-internal package prefixes
// that should NOT be reported as third-party libraries.
var stdlibPrefixes = []string{
	// Go stdlib
	"fmt", "os", "io", "net", "http", "sync", "context", "errors", "strings",
	"strconv", "encoding", "crypto", "math", "sort", "time", "log", "path",
	"bytes", "bufio", "unicode", "reflect", "runtime", "testing", "flag",
	// Python stdlib
	"os", "sys", "re", "json", "time", "math", "io", "abc", "typing",
	"collections", "functools", "itertools", "pathlib", "logging",
	// Java / Kotlin stdlib
	"java.", "javax.", "kotlin.", "android.",
	// JS/TS built-ins
	"node:", "fs", "path", "url", "http", "https", "crypto", "stream",
	// Rust std
	"std::", "core::", "alloc::",
}

func filterExternal(imports []string, lang string) []string {
	var out []string
	for _, imp := range imports {
		if !isStdlib(imp, lang) {
			out = append(out, imp)
		}
	}
	return out
}

func isStdlib(imp, lang string) bool {
	// Relative imports are always internal
	if strings.HasPrefix(imp, ".") || strings.HasPrefix(imp, "/") {
		return true
	}
	// Go: stdlib packages have no dot (e.g. "fmt", "sync") but third-party do
	if lang == "Go" {
		return !strings.Contains(imp, ".")
	}
	for _, prefix := range stdlibPrefixes {
		if imp == prefix || strings.HasPrefix(imp, prefix) {
			return true
		}
	}
	return false
}

func dedupe(ss []string) []string {
	seen := make(map[string]struct{}, len(ss))
	out := ss[:0]
	for _, s := range ss {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}
