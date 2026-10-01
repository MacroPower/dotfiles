package rmguard_test

import (
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
		"glob after slash":               {command: `rm -rf $X/*`, deny: true},
		"quoted variable, glob":          {command: `rm -rf "$X"/*`, deny: true},
		"braced variable":                {command: `rm -rf ${X}/`, deny: true},
		"double slash":                   {command: `rm -rf $X//`, deny: true},
		"escaped slash":                  {command: `rm -rf $X\/`, deny: true},
		"command substitution after":     {command: `rm -rf $X/$(basename "$p")`, deny: true},
		"empty default, top-level name":  {command: `rm -rf "${X:-}/tmp"`, deny: true},
		"variable default":               {command: `rm -rf ${X:-$Y}/`, deny: true},
		"quoted empty default":           {command: `rm -rf ${X-""}/`, deny: true},
		"top-level name with glob":       {command: `rm -rf $X/usr/*`, deny: true},
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
			command: `rm -rf $X/*`,
			want:    "write the target as `${X:?}/*`,",
		},
		"quoting is kept": {
			command: `rm -rf "$X"/*`,
			want:    "write the target as `\"${X:?}\"/*`,",
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
