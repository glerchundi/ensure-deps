package main

import (
	"errors"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml"
)

type gopkgTOMLDecl struct {
	Constraint []struct {
		Branch   string
		Name     string
		Revision string
		Version  string
	}
}

type excludes map[string]interface{}

func (e *excludes) String() string {
	return join(*e, ", ")
}

func (e *excludes) Set(value string) error {
	(*e)[value] = struct{}{}
	return nil
}

func main() {
	excludeDirs := &excludes{}
	excludeImports := &excludes{}

	flag.Var(excludeDirs, "exclude", "Exclude a file/dir from being scrapped.")
	flag.Var(excludeImports, "exclude-import", "Exclude an import from being included in the missing imports.")
	flag.Parse()

	var (
		importPaths []string
		gopkgTOML   gopkgTOMLDecl
	)

	fset := token.NewFileSet()
	err := filepath.Walk(".", func(path string, f os.FileInfo, err error) error {
		if !f.IsDir() {
			if ok, err := any(excludeDirs, func(s string) (bool, error) { return filepath.Match(s, path) }); err != nil {
				return err
			} else if ok {
				return nil
			}

			if src, err := parser.ParseFile(fset, path, nil, parser.Mode(0)); err == nil {
				for _, i := range src.Imports {
					importPath, err := strconv.Unquote(i.Path.Value)
					if err != nil {
						return err
					}

					// 😱 https://github.com/golang/tools/blob/release-branch.go1.10/go/ast/astutil/imports.go#L192-L196
					if !strings.Contains(importPath, ".") {
						continue
					}

					if ok, _ := any(excludeImports, func(s string) (bool, error) { return strings.HasPrefix(importPath, s), nil }); ok {
						continue
					}

					parts := strings.Split(importPath, "/")
					if len(parts) < 3 {
						return errors.New("unexpected import format")
					}

					importPaths = append(importPaths, strings.Join([]string{parts[0], parts[1], parts[2]}, "/"))
				}
			}
		}
		return nil
	})

	if err != nil {
		fatal("%v", err)
	}

	data, err := ioutil.ReadFile("Gopkg.toml")
	if err != nil {
		fatal("%v", err)
	}

	if err := toml.Unmarshal(data, &gopkgTOML); err != nil {
		fatal("%v", err)
	}

	depImportPaths := make(map[string]interface{})
	for _, constraint := range gopkgTOML.Constraint {
		depImportPaths[constraint.Name] = constraint
	}

	missingImportPaths := make(map[string]interface{})
	for _, importPath := range importPaths {
		if _, ok := depImportPaths[importPath]; !ok {
			missingImportPaths[importPath] = struct{}{}
		}
	}

	if len(missingImportPaths) > 0 {
		fatal("Missing import paths:\n%s\n", join(missingImportPaths, "\n"))
	}
}

func fatal(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, format, a...)
	os.Exit(1)
}

func join(set map[string]interface{}, sep string) string {
	b := strings.Builder{}

	i := 1
	n := len(set)
	for k := range set {
		b.WriteString(k)
		if i < n {
			b.WriteString(sep)
		}
		i++
	}

	return b.String()
}

func any(e *excludes, predicate func(string) (bool, error)) (bool, error) {
	for k := range *e {
		if ok, err := predicate(k); err != nil {
			return false, err
		} else if ok {
			return true, nil
		}
	}
	return false, nil
}
