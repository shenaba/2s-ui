package jsonsql

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A JSON function spelled by hand over a stored column reads a BLOB, which is
// how issue #268 took the core down. Anything that needs one goes through this
// package instead, so the CAST cannot be forgotten.
var bareJSONFunction = regexp.MustCompile(`\bjsonb?(_[a-z_]+)?\(`)

func TestNoBareJSONFunctions(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("expected the module root at %s: %v", root, err)
	}
	self, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}

	scanned := 0
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "frontend") {
				return filepath.SkipDir
			}
			if path == self {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		scanned++
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for line := 1; scanner.Scan(); line++ {
			text := scanner.Text()
			if strings.HasPrefix(strings.TrimSpace(text), "//") {
				continue
			}
			if bareJSONFunction.MatchString(text) {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s:%d: SQL JSON function outside database/jsonsql: %s", rel, line, strings.TrimSpace(text))
			}
		}
		return scanner.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	if scanned == 0 {
		t.Fatal("scanned no Go files; the walk is not looking at the module")
	}
}

func TestCallsReadTheColumnAsText(t *testing.T) {
	cases := map[string]string{
		Each("clients.inbounds"):                "json_each(CAST(clients.inbounds AS TEXT))",
		Valid("out_json"):                       "json_valid(CAST(out_json AS TEXT))",
		Extract("clients.config", "$.vless"):    "json_extract(CAST(clients.config AS TEXT), '$.vless')",
		Extract("ext", "$.license_key"):         "json_extract(CAST(ext AS TEXT), '$.license_key')",
		Extract("options", "$.domain_strategy"): "json_extract(CAST(options AS TEXT), '$.domain_strategy')",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	}
}

func TestExtractRejectsAQuoteInThePath(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("a quote in the path must not reach the SQL")
		}
	}()
	Extract("clients.config", "$.x') OR 1=1 --")
}
