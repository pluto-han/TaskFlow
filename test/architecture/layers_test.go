package architecture_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Only the migrated account chain is constrained; legacy modules are not yet layered.
func TestAccountLayers(t *testing.T) {
	const base = "github.com/pluto-han/taskflow/internal/"
	for directory, pkg := range map[string]string{
		"domain/account": "domainaccount", "application/account": "applicationaccount",
		"infra/persistence/account": "infraaccount", "infra/password": "infrapassword",
		"interfaces/http/account": "interfaceshttpaccount",
	} {
		files, err := os.ReadDir(filepath.Join("../../internal", directory))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range files {
			if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			file, err := parser.ParseFile(token.NewFileSet(), filepath.Join("../../internal", directory, entry.Name()), nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			if file.Name.Name != pkg {
				t.Errorf("%s: package = %s", directory, file.Name.Name)
			}
			for _, spec := range file.Imports {
				path, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				own := strings.TrimPrefix(path, base)
				if strings.HasPrefix(directory, "domain/") && strings.Contains(strings.Split(path, "/")[0], ".") && !strings.HasPrefix(path, base+"domain/") {
					t.Errorf("domain imports %s", path)
				}
				if strings.HasPrefix(directory, "application/") && strings.Contains(strings.Split(path, "/")[0], ".") && !strings.HasPrefix(path, base+"domain/") {
					t.Errorf("application imports %s", path)
				}
				if strings.HasPrefix(directory, "interfaces/") && strings.HasPrefix(path, base+"infra/") {
					t.Errorf("HTTP imports concrete infrastructure %s", path)
				}
				for prefix, alias := range map[string]string{"domain/account": "domainaccount", "application/account": "applicationaccount", "infra/persistence/account": "infraaccount", "infra/password": "infrapassword"} {
					if own == prefix && (spec.Name == nil || spec.Name.Name != alias) {
						t.Errorf("%s: import %s must use alias %s", directory, path, alias)
					}
				}
			}
		}
	}
}

// Account source directories must not regain colocated tests.
func TestAccountTestsAreCentralized(t *testing.T) {
	for _, directory := range []string{
		"domain/account", "application/account", "infra/persistence/account", "infra/password", "interfaces/http/account",
	} {
		err := filepath.WalkDir(filepath.Join("../../internal", directory), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), "_test.go") {
				t.Errorf("account test outside root test/: %s", path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
