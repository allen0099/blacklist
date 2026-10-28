package filter_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/allen0099/blacklist/internal/filter"
)

// gitTree is the file tree every git compatibility case runs against.
var gitTree = []string{
	"index.html", "in12x.html", "inabx.html", "inx.html",
	"tests/t1.py", "tests/test_a.py", "robot/tests/r1.robot", "robot/tests/test_b.py",
	"app/main.js", "app.js", "sub/app/x.js", "sub/app.js", "sub/keep.log",
	"docs/a.md", "docs/a/b.md", "debugx.log", "debug1.log", "app0.log", "app3.log",
	"src/lib/m.go", "src/x.go", "build/out/o.bin", "node_modules/x/i.js",
	"logs/keep.log", "logs/drop.log", "logs/sub/a.log", "a/b/c/d.txt", "a/keep",
	"foo/bar.txt", "bar/foo.txt", ".hidden/s", "sp ace.txt", "#hash", "!bang",
	"trail", "br{a,b}/x", "star*",
}

// TestGitCompat checks that pattern matching agrees with git itself, both
// for -e pattern files and for --use-gitignore.
func TestGitCompat(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil || runtime.GOOS == "windows" {
		t.Skip("needs git and a filesystem allowing names like star*")
	}

	tests := []struct {
		name       string
		gitignores map[string]string // path -> content; "" is the root
	}{
		{"single ?", map[string]string{"": "in??x.html"}},
		{"slash anchors", map[string]string{"": "tests/*"}},
		{"leading slash", map[string]string{"": "/app"}},
		{"name at any depth", map[string]string{"": "app"}},
		{"dir only", map[string]string{"": "app/"}},
		{"bare dir name", map[string]string{"": "src"}},
		{"missing dir", map[string]string{"": "log/"}},
		{"extension", map[string]string{"": "*.html"}},
		{"char class", map[string]string{"": "app[012].log"}},
		{"char range", map[string]string{"": "debug[a-z].log"}},
		{"negated class", map[string]string{"": "[!a]*.log"}},
		{"node_modules", map[string]string{"": "node_modules"}},
		{"dir plus deep glob", map[string]string{"": "tests/\n**/test_*.py"}},
		{"one level", map[string]string{"": "docs/*.md"}},
		{"negation", map[string]string{"": "*.js\n!app.js"}},
		{"negation in dir", map[string]string{"": "logs/*\n!logs/keep.log"}},
		{"no negation under excluded dir", map[string]string{"": "logs/\n!logs/keep.log"}},
		{"negated parent only", map[string]string{"": "*.log\n!logs/*"}},
		{"leading **", map[string]string{"": "**/foo"}},
		{"middle **", map[string]string{"": "a/**/d.txt"}},
		{"trailing **", map[string]string{"": "a/**"}},
		{"trailing ** with negation", map[string]string{"": "a/**\n!a/keep"}},
		{"anchored glob", map[string]string{"": "/*.log"}},
		{"build/**", map[string]string{"": "build/**"}},
		{"tests/**", map[string]string{"": "tests/**"}},
		{"everything", map[string]string{"": "**"}},
		{"escaped #", map[string]string{"": `\#hash`}},
		{"escaped !", map[string]string{"": `\!bang`}},
		{"escaped *", map[string]string{"": `star\*`}},
		{"trailing spaces", map[string]string{"": "trail   "}},
		{"negate subpaths", map[string]string{"": "*.txt\n!foo/*"}},
		{"inner path", map[string]string{"": "b/c"}},
		{"** dir", map[string]string{"": "**/b/c/"}},
		{"dot dir", map[string]string{"": ".hidden"}},
		{"deep js", map[string]string{"": "sub/**/*.js"}},
		{"? suffix", map[string]string{"": "debug?.log"}},
		{"braces are literal", map[string]string{"": "br{a,b}"}},
		{"case sensitive", map[string]string{"": "*.LOG"}},
		{"whitelist", map[string]string{"": "*\n!*/\n!*.go"}},
		{"whitelist two types", map[string]string{"": "*\n!*/\n!*.py\n!*.robot"}},
		{"comments and blanks", map[string]string{"": "# c\n\n*.html\n"}},
		{"crlf", map[string]string{"": "*.html\r\n*.js\r\n"}},
		{"nested", map[string]string{"": "*.log", "sub/": "/app/"}},
		{"nested re-include", map[string]string{"": "*.log", "sub/": "!keep.log"}},
		{"nested anchoring", map[string]string{"": "/x.js", "sub/app/": "/x.js"}},
		{"nested cannot undo dir", map[string]string{"": "logs/", "logs/": "!keep.log"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := makeTree(t, gitTree...)
			for dir, content := range tc.gitignores {
				p := filepath.Join(root, filepath.FromSlash(dir), ".gitignore")
				if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			git(t, root, "init", "-q")
			out := git(t, root, "ls-files", "-z", "--others", "--exclude-standard")
			want := withoutGitignores(strings.Split(strings.TrimSuffix(out, "\x00"), "\x00"))
			want = slices.DeleteFunc(want, func(p string) bool { return p == "" })
			slices.Sort(want)
			wantStr := strings.Join(want, " ")

			results, err := filter.Run(filter.Options{Dir: root, UseGitignore: true})
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(files(results), " "); got != wantStr {
				t.Errorf("--use-gitignore:\n got  %s\n want %s", got, wantStr)
			}

			if len(tc.gitignores) > 1 {
				return // nested files have no -e equivalent
			}
			ef := filepath.Join(t.TempDir(), "exclude")
			if err := os.WriteFile(ef, []byte(tc.gitignores[""]), 0o644); err != nil {
				t.Fatal(err)
			}
			results, err = filter.Run(filter.Options{
				Dir:             root,
				ExcludeFiles:    []string{ef},
				ExcludePatterns: []string{"/.git/", "/.gitignore"},
			})
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(files(results), " "); got != wantStr {
				t.Errorf("-e:\n got  %s\n want %s", got, wantStr)
			}
		})
	}
}

// git runs git in dir, isolated from the user's and system configuration.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "core.excludesFile=" + os.DevNull}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}

// files returns the sorted included files, without .gitignore files.
func files(results []filter.Result) []string {
	var out []string
	for _, r := range results {
		if r.Included() && !r.IsDir {
			out = append(out, r.Rel)
		}
	}
	out = withoutGitignores(out)
	slices.Sort(out)
	return out
}

func withoutGitignores(paths []string) []string {
	return slices.DeleteFunc(paths, func(p string) bool {
		return p == ".gitignore" || strings.HasSuffix(p, "/.gitignore")
	})
}
