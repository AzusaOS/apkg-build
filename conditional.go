package main

import "strings"

// filterConditionals takes a list of strings that may contain conditional
// prefixes and returns only those that match the current build environment.
// Format: "VAR:value?actual_content" - include actual_content only if vars[VAR] == value
// No prefix means always included.
func (e *buildEnv) filterConditionals(items []string) []string {
	var result []string
	for _, item := range items {
		p := strings.IndexByte(item, '?')
		if p == -1 {
			result = append(result, item)
			continue
		}

		condition := item[:p]
		content := item[p+1:]

		cp := strings.IndexByte(condition, ':')
		if cp == -1 {
			// no colon = not a conditional, include as-is
			result = append(result, item)
			continue
		}

		varName := condition[:cp]
		varValue := condition[cp+1:]

		if e.getVar(varName) == varValue {
			result = append(result, content)
		}
	}
	return result
}

// applyConditionals filters all list fields in the build instructions
// based on the current build environment (ARCH, OS, BITS, etc).
func (e *buildEnv) applyConditionals() {
	if e.i == nil {
		return
	}
	e.i.Env = e.filterConditionals(e.i.Env)
	e.i.Source = e.filterConditionals(e.i.Source)
	e.i.SourceScript = e.filterConditionals(e.i.SourceScript)
	e.i.Patches = e.filterConditionals(e.i.Patches)
	e.i.Options = e.filterConditionals(e.i.Options)
	e.i.Arguments = e.filterConditionals(e.i.Arguments)
	e.i.ConfigurePre = e.filterConditionals(e.i.ConfigurePre)
	e.i.ConfigurePost = e.filterConditionals(e.i.ConfigurePost)
	e.i.CompilePre = e.filterConditionals(e.i.CompilePre)
	e.i.CompilePost = e.filterConditionals(e.i.CompilePost)
	e.i.InstallPre = e.filterConditionals(e.i.InstallPre)
	e.i.InstallPost = e.filterConditionals(e.i.InstallPost)
	e.i.PostLoad = e.filterConditionals(e.i.PostLoad)
}
