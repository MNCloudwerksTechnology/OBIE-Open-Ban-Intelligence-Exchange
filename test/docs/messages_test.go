package docs

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// messagesPath is the inventory of the messages the tools and the node
// show to people (ADR 0028).
const messagesPath = "documentation/operations/messages.md"

// inventoryRow is a table row of the inventory; its first cell names the
// message in backticks.
var inventoryRow = regexp.MustCompile("^\\| `([^`]+)` \\|")

// logFuncs are the calls that log a warning or an error; the console's
// warn drops warnings that clients provoke too often.
var logFuncs = map[string]bool{"Warn": true, "Error": true, "warn": true}

// TestMessageInventory keeps the message inventory in step with the code
// in both directions: every problem ID of obied and obiectl and every
// warning and error the node logs has a row, and every row names one that
// exists.
func TestMessageInventory(t *testing.T) {
	doc := readRepoFile(t, messagesPath)
	for _, inv := range []struct {
		section string
		code    map[string]string // message -> where the code has it
	}{
		{"Command-line errors", problemIDs(t)},
		{"Log messages of the node", logMessages(t)},
	} {
		body, ok := section(doc, inv.section)
		if !ok {
			t.Errorf("%s has no section %q", messagesPath, inv.section)
			continue
		}
		rows := map[string]bool{}
		for _, line := range strings.Split(body, "\n") {
			if m := inventoryRow.FindStringSubmatch(line); m != nil {
				rows[m[1]] = true
			}
		}
		if len(inv.code) == 0 {
			t.Errorf("%s: found no messages in the code", inv.section)
		}
		for msg, where := range inv.code {
			if !rows[msg] {
				t.Errorf("%s: %q (%s) has no row in %s", inv.section, msg, where, messagesPath)
			}
		}
		for msg := range rows {
			if _, ok := inv.code[msg]; !ok {
				t.Errorf("%s: the row %q of %s names nothing in the code", inv.section, msg, messagesPath)
			}
		}
	}
}

// goPackage is the parsed non-test Go files of one directory.
type goPackage struct {
	fset  *token.FileSet
	files []*ast.File
	// consts are the package's string constants by name.
	consts map[string]string
}

// parseDir parses the non-test Go files of dir, relative to the
// repository root.
func parseDir(t *testing.T, dir string) goPackage {
	t.Helper()
	pkg := goPackage{fset: token.NewFileSet(), consts: map[string]string{}}
	paths, err := filepath.Glob(filepath.Join(repoRoot, dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(pkg.fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		pkg.files = append(pkg.files, f)
	}
	for _, f := range pkg.files {
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, name := range vs.Names {
					if i < len(vs.Values) {
						if s, ok := pkg.constString(vs.Values[i]); ok {
							pkg.consts[name.Name] = s
						}
					}
				}
			}
		}
	}
	return pkg
}

// constString returns the value of a constant string expression: literals
// and the package's string constants, joined with +.
func (p goPackage) constString(e ast.Expr) (string, bool) {
	switch e := e.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(e.Value)
		return s, err == nil
	case *ast.Ident:
		s, ok := p.consts[e.Name]
		return s, ok
	case *ast.ParenExpr:
		return p.constString(e.X)
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}
		x, ok := p.constString(e.X)
		if !ok {
			return "", false
		}
		y, ok := p.constString(e.Y)
		return x + y, ok
	}
	return "", false
}

// where returns the position of n relative to the repository root.
func (p goPackage) where(n ast.Node) string {
	pos := p.fset.Position(n.Pos())
	rel, err := filepath.Rel(repoRoot, pos.Filename)
	if err != nil {
		rel = pos.Filename
	}
	return filepath.ToSlash(rel) + ":" + strconv.Itoa(pos.Line)
}

// problemIDs returns the ID of every problem the command-line tools
// explain (internal/cli, type problem). An ID must be a string literal, so
// that this test can find it.
func problemIDs(t *testing.T) map[string]string {
	t.Helper()
	pkg := parseDir(t, "internal/cli")
	ids := map[string]string{}
	for _, f := range pkg.files {
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			if typ, ok := lit.Type.(*ast.Ident); !ok || typ.Name != "problem" {
				return true
			}
			for _, elt := range lit.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if key, ok := kv.Key.(*ast.Ident); !ok || key.Name != "id" {
					continue
				}
				value, ok := kv.Value.(*ast.BasicLit)
				if !ok || value.Kind != token.STRING {
					t.Errorf("%s: a problem's id must be a string literal", pkg.where(kv))
					continue
				}
				id, err := strconv.Unquote(value.Value)
				if err != nil {
					t.Fatal(err)
				}
				ids[id] = pkg.where(kv)
			}
			return true
		})
	}
	return ids
}

// logMessages returns every warning and error message the node logs with
// a constant text, below internal/.
func logMessages(t *testing.T) map[string]string {
	t.Helper()
	msgs := map[string]string{}
	err := filepath.WalkDir(filepath.Join(repoRoot, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		pkg := parseDir(t, rel)
		for _, f := range pkg.files {
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) == 0 {
					return true
				}
				if sel, ok := call.Fun.(*ast.SelectorExpr); !ok || !logFuncs[sel.Sel.Name] {
					return true
				}
				if msg, ok := pkg.constString(call.Args[0]); ok {
					msgs[msg] = pkg.where(call)
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return msgs
}
