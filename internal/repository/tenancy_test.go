package repository

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEveryQueryFiltersByOrg parses every Go file in this package and,
// for each top-level function, collects every SQL-shaped string literal
// reachable from that function — directly written, OR referenced via a
// package-level const/var whose value is a string. If any of those
// literals touch a tenant-scoped table, the function as a whole must
// mention organization_id somewhere (in any of those literals).
//
// This is the "discipline" half of the app-layer tenancy model — the
// repository can't fall back to RLS if a future query forgets the filter,
// so the test enforces it at compile time.
//
// Functions on the explicit allow-list are exempt: they cross orgs by
// design (auth-time api-key lookup) or operate on system/identity tables.
func TestEveryQueryFiltersByOrg(t *testing.T) {
	allowedFunctions := map[string]bool{
		"FindAPIKeyByHash":            true, // pre-principal auth lookup
		"TouchAPIKey":                 true, // already constrained to a single id
		"GetUserByID":                 true, // identity table
		"GetUserByClerkID":            true,
		"UpsertUser":                  true,
		"DeleteUserByClerkID":         true,
		"GetOrganization":             true,
		"GetOrganizationByClerkID":    true,
		"UpsertOrganization":          true,
		"DeleteOrganizationByClerkID": true,
		"ListMembershipsForUser":      true,
		"UpsertMembership":            true,
		"DeleteMembership":            true,
		"RecordWebhookEvent":          true,
		"UpsertWorkerHeartbeat":       true,
		"ListWorkerHeartbeats":        true,
		"DeleteWorkerHeartbeat":       true,
		"LogNotificationEvent":        true, // audit table; opaque to users
		"CountConsecutiveFailures":    true, // scoped via monitor_id (FK)
		"GetOpenIncident":             true, // scoped via monitor_id (FK)
		"Ping":                        true,
	}

	tenantTables := map[string]bool{
		"monitors":              true,
		"check_results":         true,
		"incidents":             true,
		"notification_channels": true,
		"api_keys":              true,
	}

	dir := "."
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	fset := token.NewFileSet()
	files := []*ast.File{}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		files = append(files, f)
	}

	// Collect package-level string consts/vars: name → unquoted value.
	stringDecls := map[string]string{}
	for _, f := range files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					raw, _ := unquoteSQL(lit.Value)
					stringDecls[name.Name] = raw
				}
			}
		}
	}

	for _, f := range files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if allowedFunctions[fn.Name.Name] {
				continue
			}

			var literals []string
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.BasicLit:
					if x.Kind == token.STRING {
						raw, _ := unquoteSQL(x.Value)
						literals = append(literals, strings.ToLower(raw))
					}
				case *ast.Ident:
					if val, ok := stringDecls[x.Name]; ok {
						literals = append(literals, strings.ToLower(val))
					}
				}
				return true
			})

			touched := map[string]bool{}
			anyMentionsOrg := false
			for _, lit := range literals {
				if strings.Contains(lit, "organization_id") {
					anyMentionsOrg = true
				}
				if !looksLikeSQL(lit) {
					continue
				}
				for tbl := range tenantTables {
					if mentionsTable(lit, tbl) {
						touched[tbl] = true
					}
				}
			}
			if len(touched) == 0 || anyMentionsOrg {
				continue
			}

			tables := make([]string, 0, len(touched))
			for tbl := range touched {
				tables = append(tables, tbl)
			}
			t.Errorf("%s: function %s touches tenant tables %v but never references organization_id; either filter by it or add the function name to the allow-list with a justification comment",
				fset.Position(fn.Pos()), fn.Name.Name, tables)
		}
	}
}

func unquoteSQL(s string) (string, error) {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '`') && s[0] == s[len(s)-1] {
		inner := s[1 : len(s)-1]
		if s[0] == '`' {
			return inner, nil
		}
		inner = strings.ReplaceAll(inner, `\n`, "\n")
		inner = strings.ReplaceAll(inner, `\t`, "\t")
		inner = strings.ReplaceAll(inner, `\"`, `"`)
		return inner, nil
	}
	return s, nil
}

func looksLikeSQL(lower string) bool {
	if !strings.Contains(lower, "select ") && !strings.Contains(lower, "insert ") &&
		!strings.Contains(lower, "update ") && !strings.Contains(lower, "delete ") {
		return false
	}
	return strings.Contains(lower, "from ") || strings.Contains(lower, "into ") ||
		strings.Contains(lower, "update ") || strings.Contains(lower, "delete from ")
}

func mentionsTable(lower, tbl string) bool {
	for _, prefix := range []string{"from ", "into ", "update ", "join "} {
		idx := 0
		for {
			i := strings.Index(lower[idx:], prefix+tbl)
			if i < 0 {
				break
			}
			pos := idx + i + len(prefix) + len(tbl)
			if pos == len(lower) || !isIdentRune(lower[pos]) {
				return true
			}
			idx = pos
		}
	}
	return false
}

func isIdentRune(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '_'
}
