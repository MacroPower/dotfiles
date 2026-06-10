package kubectx

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// HasKubectl walks the AST looking for commands where the first word is
// exactly "kubectl".
func HasKubectl(prog *syntax.File) bool {
	found := false

	syntax.Walk(prog, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) < 1 {
			return true
		}

		parts0 := call.Args[0].Parts
		if len(parts0) != 1 {
			return true
		}

		lit, ok := parts0[0].(*syntax.Lit)
		if !ok || lit.Value != "kubectl" {
			return true
		}

		found = true

		return true
	})

	return found
}

// KubeconfigOverride walks the AST looking for a kubectl call that
// points itself at a kubeconfig other than the session-scoped one,
// either via an inline KUBECONFIG= assignment or a --kubeconfig flag.
// Returns an actionable reason and true on the first such call.
//
// The session kubeconfig is already scoped to the context chosen via
// mcp__kubectx__select, so an override is the documented escape hatch
// this check closes. Wrapper forms (env, sudo, sh -c, unset) are out of
// scope here and are contained by the sandbox read-deny on ~/.kube.
func KubeconfigOverride(prog *syntax.File) (string, bool) {
	const reason = "This kubectl command overrides the session kubeconfig (KUBECONFIG= or --kubeconfig). " +
		"The session is already scoped to the context chosen via mcp__kubectx__select; " +
		"use mcp__kubectx__select to switch contexts instead of pointing kubectl at another kubeconfig."

	overridden := false

	syntax.Walk(prog, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) < 1 {
			return true
		}

		parts0 := call.Args[0].Parts
		if len(parts0) != 1 {
			return true
		}

		lit, ok := parts0[0].(*syntax.Lit)
		if !ok || lit.Value != "kubectl" {
			return true
		}

		for _, assign := range call.Assigns {
			if assign.Name != nil && assign.Name.Value == "KUBECONFIG" {
				overridden = true
				return false
			}
		}

		for _, arg := range call.Args[1:] {
			if wordIsKubeconfigFlag(arg) {
				overridden = true
				return false
			}
		}

		return true
	})

	if overridden {
		return reason, true
	}

	return "", false
}

// wordIsKubeconfigFlag reports whether word is a --kubeconfig flag token,
// in either the separate-value form (--kubeconfig) or the inline-value
// form (--kubeconfig=...). Only the first literal part is inspected, so
// the inline form is caught even when its value is an expansion
// (--kubeconfig=$VAR parses as [Lit("--kubeconfig="), ParamExp]). The
// flag token itself must be a literal; its value may be any word, which
// also covers the separate-value expansion form (--kubeconfig $VAR).
func wordIsKubeconfigFlag(word *syntax.Word) bool {
	if len(word.Parts) == 0 {
		return false
	}

	lit, ok := word.Parts[0].(*syntax.Lit)
	if !ok {
		return false
	}

	return lit.Value == "--kubeconfig" || strings.HasPrefix(lit.Value, "--kubeconfig=")
}
