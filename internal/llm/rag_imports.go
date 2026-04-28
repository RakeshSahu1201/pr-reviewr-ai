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
