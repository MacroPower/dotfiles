package rmguard_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"mvdan.cc/sh/v3/syntax"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/rmguard"
)

// mustParse parses command into a shell AST, failing the test on a
// parse error.
func mustParse(t *testing.T, command string) *syntax.File {
	t.Helper()

	prog, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	require.NoError(t, err)

	return prog
}

// TestCheck pins which rm and rmdir operands the guard denies.
func TestCheck(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		command string
		deny    bool
	}{
		"two variables":                  {command: `rm -rf $S/$d`, deny: true},
		"quoted variable, bare slash":    {command: `rm -rf "$X"/`, deny: true},
		"suffix glob after slash":        {command: `rm -rf $X/*.log`, deny: true},
		"braced variable":                {command: `rm -rf ${X}/`, deny: true},
		"double slash":                   {command: `rm -rf $X//`, deny: true},
		"escaped slash":                  {command: `rm -rf $X\/`, deny: true},
		"command substitution after":     {command: `rm -rf $X/$(basename "$p")`, deny: true},
		"empty default, top-level name":  {command: `rm -rf "${X:-}/tmp"`, deny: true},
		"variable default":               {command: `rm -rf ${X:-$Y}/`, deny: true},
		"quoted empty default":           {command: `rm -rf ${X-""}/`, deny: true},
		"positional parameter":           {command: `rm -rf $1/`, deny: true},
		"operand after double dash":      {command: `rm -rf -- $X/`, deny: true},
		"dash operand after double dash": {command: `rm -rf -- -x $X/`, deny: true},
		"later operand":                  {command: `rm -rf ./a $X/`, deny: true},
		"rm by path":                     {command: `/bin/rm -rf $X/`, deny: true},
		"quoted command word":            {command: `'rm' -rf $X/`, deny: true},
		"rmdir":                          {command: `rmdir $X/`, deny: true},
		"inside a for loop":              {command: `for d in a b; do rm -rf $S/$d; done`, deny: true},
		"inside a subshell":              {command: `(cd /x && rm -rf $X/)`, deny: true},
		"later call":                     {command: `rm -f ./a; rm -rf $X/`, deny: true},

		"subdirectory below a variable":    {command: `rm -rf "$TMPDIR/build"`},
		"top-level name with deeper path":  {command: `rm -rf "$X/tmp/foo"`},
		"top-level name then expansion":    {command: `rm -rf $X/tmp$y`},
		"dotfile below home":               {command: `rm -rf "$HOME/.cache/x"`},
		"variable is the whole operand":    {command: `rm -f "$f"`},
		"unslashed variable":               {command: `rm -rf $X`},
		"single-quoted":                    {command: `rm -rf '$X/'`},
		"leading command substitution":     {command: `rm -rf $(pwd)/`},
		"suffix-trimmed variable":          {command: `rm -rf ${X%/}/`},
		"length expansion":                 {command: `rm -rf ${#X}/`},
		"indirect expansion":               {command: `rm -rf ${!X}/`},
		"array index":                      {command: `rm -rf ${X[0]}/`},
		"literal default":                  {command: `rm -rf ${X:-/srv/data}/`},
		"error on empty":                   {command: `rm -rf "${S:?}/${d:?}"`},
		"error on unset":                   {command: `rm -rf ${X?}/`},
		"relative literal":                 {command: `rm -rf ./x`},
		"literal prefix before a variable": {command: `rm -rf build/$X/`},
		"rm as an argument":                {command: `echo rm -rf $X/`},
		"other command":                    {command: `ls $X/`},
		"variable in flag position":        {command: `rm -$X/ ./a`},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			reason, deny := rmguard.Check(mustParse(t, tc.command), tc.command)
			assert.Equal(t, tc.deny, deny)

			if tc.deny {
				assert.True(t, strings.HasPrefix(reason, rmguard.ReasonPrefix), reason)
			} else {
				assert.Empty(t, reason)
			}
		})
	}
}

// TestCheckReason pins the rewrite the deny reason suggests.
func TestCheckReason(t *testing.T) {
	t.Parallel()

	const generic = "add `:?` to the leading variable"

	cases := map[string]struct {
		command string
		want    string
	}{
		"each variable gains the guard": {
			command: `rm -rf $S/$d`,
			want:    "write the target as `${S:?}/${d:?}`,",
		},
		"glob stays unquoted": {
			command: `rm -rf $X/*.log`,
			want:    "write the target as `${X:?}/*.log`,",
		},
		"quoting is kept": {
			command: `rm -rf "$X"/`,
			want:    "write the target as `\"${X:?}\"/`,",
		},
		"guarded variables are left alone": {
			command: `rm -rf "$S/${d:?}"`,
			want:    "write the target as `\"${S:?}/${d:?}\"`,",
		},
		"first offending operand is named": {
			command: `rm -rf ./a $X/ $Y/`,
			want:    "targets `$X/`,",
		},
		"default form gets the generic advice": {
			command: `rm -rf ${X:-}/`,
			want:    generic,
		},
		"positional parameter gets the generic advice": {
			command: `rm -rf $1/`,
			want:    generic,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			reason, deny := rmguard.Check(mustParse(t, tc.command), tc.command)
			require.True(t, deny)
			assert.Contains(t, reason, tc.want)
			assert.Contains(t, reason, "literal path")
		})
	}
}

// TestCheckRewrite pins that the guard accepts the target its own deny
// reason suggests.
func TestCheckRewrite(t *testing.T) {
	t.Parallel()

	suggestion := regexp.MustCompile("write the target as `([^`]+)`")

	operands := map[string]string{
		"two variables":      `$S/$d`,
		"quoted variable":    `"$X"/`,
		"braced variable":    `${X}/`,
		"suffix glob":        `$X/*.log`,
		"top-level name":     `"$X/tmp"`,
		"substitution after": `$X/$(basename "$p")`,
	}

	for name, operand := range operands {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			command := "rm -rf " + operand

			reason, deny := rmguard.Check(mustParse(t, command), command)
			require.True(t, deny)

			match := suggestion.FindStringSubmatch(reason)
			require.NotNil(t, match, reason)

			fixed := "rm -rf " + match[1]

			reason, deny = rmguard.Check(mustParse(t, fixed), fixed)
			assert.False(t, deny, reason)
		})
	}
}

// TestCheckUnresolvable pins the targets the guard denies because
// Claude Code cannot resolve them before the command runs, and the
// rule each one trips.
func TestCheckUnresolvable(t *testing.T) {
	t.Parallel()

	const (
		substitution = "output of a command substitution"
		changeDir    = "relative glob after a directory change"
		base         = "directory that is not a literal path"
		depth        = "more than one directory level"
	)

	cases := map[string]struct {
		command string
		// want is a phrase from the expected rule's reason; empty
		// means the command passes.
		want string
	}{
		"substitution":                   {command: `rm -rf $(find . -name x)`, want: substitution},
		"backtick substitution":          {command: "rm -rf `cat list`", want: substitution},
		"quoted substitution":            {command: `rm -rf "$(cat list)"`, want: substitution},
		"cd then dot glob":               {command: `cd build && rm -rf ./*`, want: changeDir},
		"cd then bare glob":              {command: `cd build; rm -rf *`, want: changeDir},
		"pushd then nested glob":         {command: `pushd x && rm -rf sub/*`, want: changeDir},
		"home glob":                      {command: `rm -rf ~/*`, want: base},
		"glob below home":                {command: `rm -rf ~/foo/*`, want: base},
		"glob below a variable path":     {command: `rm -rf "$TMPDIR/build"/*`, want: base},
		"glob below a substitution":      {command: `rm -rf $(pwd)/*`, want: base},
		"glob below a parent reference":  {command: `rm -rf ../*`, want: base},
		"directory glob":                 {command: `rm -rf */`, want: base},
		"rmdir with parents on a glob":   {command: `rmdir -p build/*`, want: base},
		"two trailing glob levels":       {command: `rm -rf build/*/*`, want: depth},
		"glob in a parent segment":       {command: `rm -rf */foo/*`, want: depth},
		"absolute two-level glob":        {command: `rm -rf /tmp/x/*/*`, want: depth},
		"later operand":                  {command: `rm -rf ./a ~/*`, want: base},
		"glob below an empty variable":   {command: `rm -rf $X/*`, want: base},
		"glob below a quoted variable":   {command: `rm -rf "$X"/*`, want: base},
		"glob below a guarded variable":  {command: `rm -rf "${X:?}"/*`, want: base},
		"top-level name with glob":       {command: `rm -rf $X/usr/*`, want: base},
		"single-level glob":              {command: `rm -rf build/*`},
		"bare glob":                      {command: `rm -rf *`},
		"dot glob":                       {command: `rm -rf ./*`},
		"absolute single-level glob":     {command: `rm -rf /tmp/x/*`},
		"suffix glob":                    {command: `rm -f *.log`},
		"quoted glob":                    {command: `rm -rf "build/*"`},
		"escaped glob":                   {command: `rm -rf build/\*`},
		"single-quoted home glob":        {command: `rm -rf '~/*'`},
		"cd then absolute glob":          {command: `cd build && rm -rf /abs/dir/*`},
		"cd then plain relative path":    {command: `cd build && rm -rf out`},
		"cd after the removal":           {command: `rm -rf build/*; cd x`},
		"path below a substitution":      {command: `rm -rf "$(pwd)/build"`},
		"rmdir with parents, no glob":    {command: `rmdir -p a/b`},
		"parent reference without glob":  {command: `rm -rf ../build`},
		"substitution with literal text": {command: `rm -rf build-$(date +%F)`},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			reason, deny := rmguard.Check(mustParse(t, tc.command), tc.command)
			if tc.want == "" {
				assert.False(t, deny, reason)
				return
			}

			require.True(t, deny)
			assert.True(t, strings.HasPrefix(reason, rmguard.UnresolvablePrefix), reason)
			assert.Contains(t, reason, tc.want)
		})
	}
}

// TestCheckInline pins how the guard reads a script that a call passes
// to a shell with `-c`, and the rule each denied script trips.
func TestCheckInline(t *testing.T) {
	t.Parallel()

	const (
		variable     = "starts with a variable expansion"
		substitution = "output of a command substitution"
		changeDir    = "relative glob after a directory change"
		base         = "directory that is not a literal path"
		depth        = "more than one directory level"
	)

	cases := map[string]struct {
		command string
		// want is a phrase from the expected rule's reason; empty
		// means the command passes.
		want string
	}{
		"single-quoted script":             {command: `sh -c 'rm -rf $X/'`, want: variable},
		"outer expansion in double quotes": {command: `bash -c "rm -rf \"$X\"/"`, want: variable},
		"escaped inner expansion":          {command: `bash -c "rm -rf \$X/"`, want: variable},
		"unquoted script word":             {command: `sh -c rm\ -rf\ \$X/`, want: variable},
		"clustered flags":                  {command: `bash -lc 'rm -rf $X/'`, want: variable},
		"option with an argument":          {command: `bash -euo pipefail -c 'rm -rf $X/'`, want: variable},
		"long option":                      {command: `bash --norc -c 'rm -rf $X/'`, want: variable},
		"shell by path":                    {command: `/bin/sh -c 'rm -rf $X/'`, want: variable},
		"zsh":                              {command: `zsh -c 'rm -rf $X/'`, want: variable},
		"find exec, outer positional": {
			command: `find . -type d -exec sh -c "rm -rf \"$1\"/" _ {} \;`,
			want:    variable,
		},
		"find exec, glob below an outer positional": {
			command: `find . -name '*.tmp' -exec sh -c "rm -rf \"$1\"/*" _ {} \;`,
			want:    base,
		},
		"xargs leaves the positional unset": {command: `xargs sh -c 'rm -rf "$1"/' _`, want: variable},
		"unbound later positional":          {command: `sh -c 'rm -rf "$2"/' _ /data`, want: variable},
		"positional bound to an expansion":  {command: `sh -c 'rm -rf "$1"/' _ "$DIR"`, want: variable},
		"positional bound to nothing":       {command: `sh -c 'rm -rf "$1"/' _ ""`, want: variable},
		"nested shells":                     {command: `sh -c "bash -c 'rm -rf \$X/'"`, want: variable},
		"inside a subshell":                 {command: `(sh -c 'rm -rf $X/')`, want: variable},
		"substitution":                      {command: `sh -c 'rm -rf $(find . -name x)'`, want: substitution},
		"outer substitution":                {command: `sh -c "rm -rf $(find . -name x)"`, want: substitution},
		"cd inside the script":              {command: `sh -c 'cd build && rm -rf ./*'`, want: changeDir},
		"cd before the shell":               {command: `cd build && sh -c 'rm -rf ./*'`, want: changeDir},
		"home glob":                         {command: `sh -c 'rm -rf ~/*'`, want: base},
		"glob below a bound positional":     {command: `sh -c 'rm -rf "$1"/*' _ {}`, want: base},
		"two glob levels":                   {command: `sh -c 'rm -rf build/*/*'`, want: depth},

		"bound positional":                {command: `sh -c 'rm -rf "$1"/' _ /data`},
		"find exec, bound positional":     {command: `find . -type d -exec sh -c 'rm -rf "$1"/' _ {} \;`},
		"bound all-arguments parameter":   {command: `sh -c 'rm -rf "$@"/' _ /data`},
		"error on empty":                  {command: `sh -c 'rm -rf "${X:?}/"'`},
		"literal path":                    {command: `sh -c 'rm -rf ./build'`},
		"rm as an argument":               {command: `sh -c 'echo rm -rf $X/'`},
		"single-quoted inside the script": {command: `sh -c "rm -rf '\$X/'"`},
		"script file, not a script":       {command: `sh run.sh -c 'rm -rf $X/'`},
		"no command flag":                 {command: `sh -x 'rm -rf $X/'`},
		"flag after double dash":          {command: `sh -- -c 'rm -rf $X/'`},
		"shell in one quoted word":        {command: `echo 'sh -c "rm -rf $X/"'`},
		"cd after the shell":              {command: `sh -c 'rm -rf ./*'; cd build`},
		"ANSI-C quoted script":            {command: `sh -c $'rm -rf $X/'`},
		"script that does not parse":      {command: `sh -c 'rm -rf $X/ ('`},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			reason, deny := rmguard.Check(mustParse(t, tc.command), tc.command)
			if tc.want == "" {
				assert.False(t, deny, reason)
				return
			}

			require.True(t, deny)
			assert.Contains(t, reason, tc.want)
		})
	}
}

// TestCheckInlineReason pins how the deny reason for an inline script
// names the shell and quotes the script's own text.
func TestCheckInlineReason(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		command string
		want    []string
	}{
		"script text and rewrite": {
			command: `bash -c "rm -rf \"$X\"/build/ \$Y/"`,
			want: []string{
				"`rm -rf \"$X\"/build/ $Y/` in the script passed to `bash -c` targets `$Y/`,",
				"write the target as `${Y:?}/`,",
			},
		},
		"shell is named as written": {
			command: `find . -exec /bin/sh -c 'rm -rf ~/*' \;`,
			want:    []string{"`rm -rf ~/*` in the script passed to `/bin/sh -c` targets `~/*`,"},
		},
		"innermost shell is named": {
			command: `sh -c "bash -c 'rm -rf \$X/'"`,
			want:    []string{"in the script passed to `bash -c`"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			reason, deny := rmguard.Check(mustParse(t, tc.command), tc.command)
			require.True(t, deny)

			for _, want := range tc.want {
				assert.Contains(t, reason, want)
			}
		})
	}
}
