package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
)

// URLMatch defines regex patterns matched against parsed URL components.
// Every non-empty field must match for the overall match to succeed.
// Empty fields match anything.
type URLMatch struct {
	Scheme   string `json:"scheme,omitempty"`
	Host     string `json:"host,omitempty"`
	Path     string `json:"path,omitempty"`
	Query    string `json:"query,omitempty"`
	Fragment string `json:"fragment,omitempty"`
}

// DenyRule blocks URLs matching its [URLMatch] patterns unless an exception
// applies.
type DenyRule struct {
	URLMatch
	Reason string     `json:"reason"`
	Except []URLMatch `json:"except,omitempty"`
}

// AllowRule permits URLs matching its [URLMatch] patterns.
type AllowRule struct {
	URLMatch
}

// rulesFile is the JSON structure read from disk.
type rulesFile struct {
	Reason string      `json:"reason,omitempty"`
	Deny   []DenyRule  `json:"deny,omitempty"`
	Allow  []AllowRule `json:"allow,omitempty"`
}

// Rules holds compiled URL rules, ready for matching.
type Rules struct {
	reason string
	deny   []compiledDeny
	allow  []compiledMatch
}

type compiledMatch struct {
	scheme   *regexp.Regexp
	host     *regexp.Regexp
	path     *regexp.Regexp
	query    *regexp.Regexp
	fragment *regexp.Regexp
}

type compiledDeny struct {
	match  compiledMatch
	reason string
	except []compiledMatch
}

// LoadRules reads a JSON rules file and compiles all regex patterns.
// Returns empty [Rules] when path is empty (no filtering).
// Fails fast on invalid regex.
func LoadRules(path string) (*Rules, error) {
	if path == "" {
		return &Rules{}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading rules file: %w", err)
	}

	var f rulesFile

	err = json.Unmarshal(data, &f)
	if err != nil {
		return nil, fmt.Errorf("parsing rules file: %w", err)
	}

	rules := &Rules{reason: f.Reason}

	for i, d := range f.Deny {
		cm, err := compileURLMatch(d.URLMatch)
		if err != nil {
			return nil, fmt.Errorf("deny rule %d: %w", i, err)
		}

		cd := compiledDeny{match: cm, reason: d.Reason}

		for j, ex := range d.Except {
			exc, err := compileURLMatch(ex)
			if err != nil {
				return nil, fmt.Errorf("deny rule %d exception %d: %w", i, j, err)
			}

			cd.except = append(cd.except, exc)
		}

		rules.deny = append(rules.deny, cd)
	}

	for i, a := range f.Allow {
		cm, err := compileURLMatch(a.URLMatch)
		if err != nil {
			return nil, fmt.Errorf("allow rule %d: %w", i, err)
		}

		rules.allow = append(rules.allow, cm)
	}

	return rules, nil
}

// Check returns a non-empty reason string if the URL is denied, or "" if
// allowed. Safe to call on a nil receiver.
func (r *Rules) Check(u *url.URL) string {
	if r == nil {
		return ""
	}

	for _, d := range r.deny {
		if !d.match.matches(u) {
			continue
		}

		excepted := false
		for _, ex := range d.except {
			if ex.matches(u) {
				excepted = true
				break
			}
		}

		if !excepted {
			return d.reason
		}
	}

	if len(r.allow) > 0 {
		for _, a := range r.allow {
			if a.matches(u) {
				return ""
			}
		}

		if r.reason != "" {
			return r.reason
		}

		return "URL not in allow list"
	}

	return ""
}

func (cm *compiledMatch) matches(u *url.URL) bool {
	if cm.scheme != nil && !cm.scheme.MatchString(u.Scheme) {
		return false
	}

	if cm.host != nil && !cm.host.MatchString(u.Host) {
		return false
	}

	if cm.path != nil && !cm.path.MatchString(u.Path) {
		return false
	}

	if cm.query != nil && !cm.query.MatchString(u.RawQuery) {
		return false
	}

	if cm.fragment != nil && !cm.fragment.MatchString(u.Fragment) {
		return false
	}

	return true
}

func compileURLMatch(m URLMatch) (compiledMatch, error) {
	var cm compiledMatch

	fields := []struct {
		pattern string
		target  **regexp.Regexp
		name    string
	}{
		{m.Scheme, &cm.scheme, "scheme"},
		{m.Host, &cm.host, "host"},
		{m.Path, &cm.path, "path"},
		{m.Query, &cm.query, "query"},
		{m.Fragment, &cm.fragment, "fragment"},
	}

	for _, f := range fields {
		re, ok, err := anchoredCompile(f.pattern)
		if err != nil {
			return cm, fmt.Errorf("%s: %w", f.name, err)
		}

		if ok {
			*f.target = re
		}
	}

	return cm, nil
}

// anchoredCompile compiles a regex pattern with implicit full-match anchoring.
// Returns (nil, false, nil) for empty patterns (wildcard).
func anchoredCompile(pattern string) (*regexp.Regexp, bool, error) {
	if pattern == "" {
		return nil, false, nil
	}

	re, err := regexp.Compile("^(?:" + pattern + ")$")
	if err != nil {
		return nil, false, fmt.Errorf("compiling pattern %q: %w", pattern, err)
	}

	return re, true, nil
}
