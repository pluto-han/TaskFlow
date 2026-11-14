package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Keep the accepted account naming and dependency checks alongside the full project checks.
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

// TestProjectLayers checks every handwritten backend package after the migration.
func TestProjectLayers(t *testing.T) {
	const base = "github.com/pluto-han/taskflow/internal/"
	err := filepath.WalkDir("../../internal", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			t.Errorf("backend test outside root test/: %s", path)
			return nil
		}
		relative, err := filepath.Rel("../../internal", path)
		if err != nil {
			return err
		}
		directory := filepath.ToSlash(filepath.Dir(relative))
		if strings.HasPrefix(directory, "db/") || directory == "httpapi" {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		parts := strings.Split(directory, "/")
		expected := parts[0] + parts[len(parts)-1]
		if parts[0] == "interfaces" {
			expected = parts[0] + parts[1] + parts[len(parts)-1]
		}
		if file.Name.Name != expected {
			t.Errorf("%s: package=%s, want %s", path, file.Name.Name, expected)
		}
		if entry.Name() == "ports.go" {
			t.Errorf("consumer contracts must remain with their owner: %s", path)
		}
		for _, spec := range file.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			external := strings.Contains(strings.Split(imported, "/")[0], ".")
			own := strings.TrimPrefix(imported, base)
			switch parts[0] {
			case "domain":
				if external && !strings.HasPrefix(imported, base+"domain/") {
					t.Errorf("%s: domain imports %s", path, imported)
				}
			case "application":
				if external && !strings.HasPrefix(imported, base+"domain/") && !strings.HasPrefix(imported, base+"application/") {
					t.Errorf("%s: application imports %s", path, imported)
				}
			case "interfaces":
				if strings.HasPrefix(own, "infra/") || strings.HasPrefix(own, "db/") {
					t.Errorf("%s: interface imports concrete adapter %s", path, imported)
				}
			case "infra":
				if strings.HasPrefix(own, "interfaces/") {
					t.Errorf("%s: infrastructure imports entry adapter %s", path, imported)
				}
			default:
				t.Errorf("unmigrated backend package: %s", directory)
			}
		}
		if parts[0] == "domain" {
			ast.Inspect(file, func(node ast.Node) bool {
				field, ok := node.(*ast.Field)
				if ok && field.Tag != nil && (strings.Contains(field.Tag.Value, "json:") || strings.Contains(field.Tag.Value, "binding:")) {
					t.Errorf("%s: transport tags in domain", path)
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
