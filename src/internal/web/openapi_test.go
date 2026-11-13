package web

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/apitoken"
)

// declaredRoutes reads apiRoutes' source: every mux.HandleFunc("METHOD
// path", s.scoped(apitoken.ScopeX, ...)) as "METHOD path" -> scope. Reading
// the declarations, not a list kept beside them, is what makes this a check
// of the routes rather than of a copy.
func declaredRoutes(t *testing.T) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "api_routes.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	scopes := map[string]string{
		"ScopeRead": string(apitoken.ScopeRead), "ScopeSync": string(apitoken.ScopeSync),
		"ScopeConfigWrite": string(apitoken.ScopeConfigWrite), "ScopeAdmin": string(apitoken.ScopeAdmin),
	}
	out := map[string]string{}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "HandleFunc" || len(call.Args) != 2 {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok {
			return true // the catch-all, apiPrefix
		}
		pattern, err := strconv.Unquote(lit.Value)
		if err != nil {
			t.Fatal(err)
		}
		wrap, ok := call.Args[1].(*ast.CallExpr)
		if !ok || len(wrap.Args) != 2 {
			t.Errorf("%s is not wrapped in s.scoped(scope, handler)", pattern)
			return true
		}
		scope, ok := wrap.Args[0].(*ast.SelectorExpr)
		if !ok || scopes[scope.Sel.Name] == "" {
			t.Errorf("%s: unrecognised scope", pattern)
			return true
		}
		out[pattern] = scopes[scope.Sel.Name]
		return true
	})
	return out
}

var pathParam = regexp.MustCompile(`\{[^}]+\}`)

// The OpenAPI document and the real routes agree, both ways: every declared
// route is in the document with the same scope, every documented operation
// is declared, and the live mux routes each documented operation to exactly
// that pattern.
func TestOpenAPIMatchesTheRoutes(t *testing.T) {
	var doc struct {
		OpenAPI string                               `json:"openapi"`
		Paths   map[string]map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(openAPIDoc, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.OpenAPI != "3.1.0" {
		t.Fatalf("openapi %q", doc.OpenAPI)
	}
	documented := map[string]string{}
	for path, ops := range doc.Paths {
		for method, op := range ops {
			scope, _ := op["x-scope"].(string)
			documented[strings.ToUpper(method)+" "+path] = scope
		}
	}
	declared := declaredRoutes(t)
	if len(declared) < 20 || len(documented) < 20 {
		t.Fatalf("read %d declared routes and %d documented operations: the scan lost them", len(declared), len(documented))
	}
	for route, scope := range declared {
		if got, ok := documented[route]; !ok {
			t.Errorf("%s is routed but not in openapi.json", route)
		} else if got != scope {
			t.Errorf("%s needs %s but openapi.json says x-scope %q", route, scope, got)
		}
	}
	for route := range documented {
		if _, ok := declared[route]; !ok {
			t.Errorf("openapi.json documents %s, which is not routed", route)
		}
	}

	f := newAPIFixture(t)
	var routes []string
	for route := range documented {
		routes = append(routes, route)
	}
	sort.Strings(routes)
	for _, route := range routes {
		method, path, _ := strings.Cut(route, " ")
		req := httptest.NewRequest(method, "http://"+testHost+pathParam.ReplaceAllString(path, "x"), nil)
		if _, pattern := f.s.apiMux.Handler(req); pattern != route {
			t.Errorf("the mux routes %s to %q", route, pattern)
		}
	}
	if _, pattern := f.s.apiMux.Handler(httptest.NewRequest(http.MethodGet, "http://"+testHost+"/api/v1/undocumented", nil)); pattern != apiPrefix {
		t.Errorf("an unknown path reached %q, not the catch-all", pattern)
	}
}
