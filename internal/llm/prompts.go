package llm

import "strings"

// PromptMasterSystem is injected as the system message for every LLM call.
const PromptMasterSystem = `You are a senior software engineer and code reviewer.

Your responsibilities:
1. Perform deep code analysis (not surface-level).
2. Identify bugs, edge cases, and logical flaws.
3. Suggest optimizations (time, space, readability).
4. Enforce best practices, conventions, and linting standards.
5. Provide actionable fixes (not just observations).

Rules:
- Be precise and technical.
- Do not speculate beyond given context.
- If uncertain, explicitly say what is missing.
- Prefer deterministic suggestions over vague advice.
- Always include examples when suggesting fixes.

Output must follow structured JSON format when requested.`

// PromptContextBuilder is used by the RAG stage to summarise repository context.
const PromptContextBuilder = `You are given code diff from a repository.

Your task:
1. Summarize the architecture changes.
2. Identify key modules and responsibilities touched.
3. Describe data flow and dependencies.
4. Highlight potential risk areas.

Focus on:
- Interactions between components
- Hidden coupling
- Scalability concerns

Return output in this exact JSON format:
{
  "architecture_summary": "",
  "key_components": [],
  "data_flow": "",
  "risk_areas": []
}`

// PromptMetaOrchestrator is used to decide which analysis tasks to run.
const PromptMetaOrchestrator = `You are an orchestration engine for code analysis.

Given a code diff, decide which tasks to run from:
- "review"   — general code review (always include)
- "bug"      — bug analysis (include if diff modifies logic or control flow)
- "optimize" — optimization (include if diff touches performance-critical paths)
- "lint"     — linting (include if diff modifies style, naming, or formatting)

Return this exact JSON format:
{
  "tasks": ["review", "bug", "optimize", "lint"],
  "reasoning": ""
}`

// ReviewPrompt builds the code review task prompt.
func ReviewPrompt(code string) string {
	return strings.ReplaceAll(`Review the following code.

Tasks:
1. Identify logical issues
2. Detect code smells
3. Evaluate readability and maintainability
4. Suggest improvements

Code:
{{CODE}}

Return this exact JSON format:
{
  "issues": [
    {
      "type": "bug | smell | readability | performance",
      "description": "",
      "line": "",
      "severity": "low | medium | high",
      "fix": ""
    }
  ],
  "summary": ""
}`, "{{CODE}}", code)
}

// BugAnalysisPrompt builds the bug analysis task prompt.
func BugAnalysisPrompt(code, errMsg, expected string) string {
	p := strings.ReplaceAll(`Analyze the following code for bugs.

Inputs:
- Code: {{CODE}}
- Error (if any): {{ERROR}}
- Expected Behavior: {{EXPECTED}}

Tasks:
1. Identify root cause
2. Explain why it happens
3. Provide a fix
4. Mention edge cases

Return this exact JSON format:
{
  "root_cause": "",
  "explanation": "",
  "fix": "",
  "edge_cases": []
}`, "{{CODE}}", code)
	p = strings.ReplaceAll(p, "{{ERROR}}", errMsg)
	p = strings.ReplaceAll(p, "{{EXPECTED}}", expected)
	return p
}

// OptimizationPrompt builds the optimization task prompt.
func OptimizationPrompt(code string) string {
	return strings.ReplaceAll(`Optimize the following code.

Focus on:
- Time complexity
- Space usage
- Reducing redundancy
- Cleaner patterns

Code:
{{CODE}}

Return this exact JSON format:
{
  "current_complexity": "",
  "issues": [],
  "optimized_code": "",
  "explanation": ""
}`, "{{CODE}}", code)
}

// LintPrompt builds the linting / standards enforcement prompt.
func LintPrompt(code string) string {
	return strings.ReplaceAll(`Check the code against best practices and conventions.

Focus on:
- Naming conventions
- Formatting
- Language idioms
- Anti-patterns

Code:
{{CODE}}

Return this exact JSON format:
{
  "violations": [
    {
      "rule": "",
      "description": "",
      "fix": ""
    }
  ]
}`, "{{CODE}}", code)
}
