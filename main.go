package main

import (
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml"
)

type gopkgTOMLDecl struct {
	Constraint []Stanza
	Override   []Stanza
}

type Stanza struct {
	Branch   string
	Name     string
	Revision string
	Version  string
}

type excludeSet map[string]interface{}

func (e excludeSet) String() string {
	return join(e, ", ")
}

func (e excludeSet) Set(value string) error {
	e[value] = struct{}{}
	return nil
}

func main() {
	var (
		gopkgTOML            gopkgTOMLDecl
		importPaths          = make(map[string]interface{})
		dir                  = "."
		excludes             = excludeSet{}
		excludeRegexps       = []*regexp.Regexp{}
		excludeImports       = excludeSet{}
		excludeImportRegexps = []*regexp.Regexp{}
	)

	flag.Var(&excludes, "exclude", "Exclude a file/dir from being scrapped.")
	flag.Var(&excludeImports, "exclude-import", "Exclude an import from being included in the missing imports.")
	flag.Parse()

	for p := range excludes {
		excludeRegexps = append(excludeRegexps, regexp.MustCompile(p))
	}

	for p := range excludeImports {
		excludeImportRegexps = append(excludeImportRegexps, regexp.MustCompile(p))
	}

	fset := token.NewFileSet()
	err := filepath.Walk(dir, func(path string, f os.FileInfo, err error) error {
		if !f.IsDir() {
			if mustExclude(excludeRegexps, path) {
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

					if mustExclude(excludeImportRegexps, importPath) {
						continue
					}

					parts := strings.Split(importPath, "/")
					nparts := numParts(importPath)
					if len(parts) < nparts {
						return fmt.Errorf("unexpected import format: %s", importPath)
					}

					importPaths[strings.Join(parts[:nparts], "/")] = struct{}{}
				}
			}
		}
		return nil
	})

	if err != nil {
		fatal("%v\n", err)
	}

	data, err := ioutil.ReadFile(filepath.Join(dir, "Gopkg.toml"))
	if err != nil {
		fatal("%v\n", err)
	}

	if err := toml.Unmarshal(data, &gopkgTOML); err != nil {
		fatal("%v\n", err)
	}

	depImportPaths := make(map[string]interface{})
	for _, constraint := range gopkgTOML.Constraint {
		depImportPaths[constraint.Name] = constraint
	}
	for _, override := range gopkgTOML.Override {
		depImportPaths[override.Name] = override
	}

	missingImportPaths := make(map[string]interface{})
	for importPath := range importPaths {
		if _, ok := depImportPaths[importPath]; !ok {
			missingImportPaths[importPath] = struct{}{}
		}
	}

	if len(missingImportPaths) > 0 {
		fatal("Missing import paths:\n%s\n", join(missingImportPaths, "\n"))
	}
}

func numParts(importPath string) int {
	idx := strings.Index(importPath, "/")
	if idx == -1 {
		return -1
	}

	switch importPath[:idx] {
	case "google.golang.org", "gopkg.in", "cloud.google.com", "firebase.google.com":
		return 2
	case "go.opencensus.io":
		return 1
	}

	return 3
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

func mustExclude(excludes []*regexp.Regexp, s string) bool {
	for _, er := range excludes {
		if er.MatchString(s) {
			return true
		}
	}
	return false
}
