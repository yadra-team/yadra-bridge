package redact

import (
	"encoding/json"
	"os"
)

type Result struct {
	Content    string
	Count      int
	Categories []Category
}

type Engine struct {
	enabled bool
	rules   []rule
}

type customRule struct {
	Pattern     string `json:"pattern"`
	Replacement string `json:"replacement"`
	Category    string `json:"category"`
}

func New(enabled bool, categories []Category, rulesFile string) (*Engine, error) {
	all := defaultRules()
	filtered := all
	if len(categories) > 0 {
		allowed := make(map[Category]bool, len(categories))
		for _, c := range categories {
			allowed[c] = true
		}
		filtered = make([]rule, 0, len(all))
		for _, r := range all {
			if allowed[r.category] {
				filtered = append(filtered, r)
			}
		}
	}
	if rulesFile != "" {
		custom, err := loadCustomRules(rulesFile)
		if err != nil {
			return nil, err
		}
		filtered = append(filtered, custom...)
	}
	return &Engine{enabled: enabled, rules: filtered}, nil
}

func (e *Engine) Enabled() bool {
	return e.enabled
}

func (e *Engine) Redact(content string) Result {
	if !e.enabled || content == "" {
		return Result{Content: content}
	}
	result := content
	count := 0
	seen := make(map[Category]bool)
	for _, rl := range e.rules {
		locs := rl.re.FindAllStringIndex(result, -1)
		if len(locs) == 0 {
			continue
		}
		replaced := 0
		for i := len(locs) - 1; i >= 0; i-- {
			start, end := locs[i][0], locs[i][1]
			match := result[start:end]
			if rl.validate != nil && !rl.validate(match) {
				continue
			}
			result = result[:start] + rl.replacement + result[end:]
			replaced++
		}
		if replaced > 0 {
			count += replaced
			seen[rl.category] = true
		}
	}
	cats := make([]Category, 0, len(seen))
	for c := range seen {
		cats = append(cats, c)
	}
	return Result{Content: result, Count: count, Categories: cats}
}

func loadCustomRules(path string) ([]rule, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var defs []customRule
	if err := json.Unmarshal(raw, &defs); err != nil {
		return nil, err
	}
	out := make([]rule, 0, len(defs))
	for _, d := range defs {
		re, err := compilePattern(d.Pattern)
		if err != nil {
			continue
		}
		cat := Category(d.Category)
		if cat == "" {
			cat = Category("custom")
		}
		out = append(out, rule{
			category:    cat,
			re:          re,
			replacement: d.Replacement,
		})
	}
	return out, nil
}
