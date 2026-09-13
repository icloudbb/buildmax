package architecture_test

// The HTTP contract. openapi.json is the only machine-readable description of
// this server's API, and nothing regenerates it: it is written by hand beside
// the handlers it describes. These tests are what keep it honest.
//
// The drift they were written for was real. The document once carried 40 of
// 117 operations, described five `created_at` fields as integers when every
// one of them is an RFC 3339 string on the wire, and left `space_id` undeclared
// on 22 operations whose paths template it.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// deliveryRoutes serve the contract itself rather than being part of it.
var deliveryRoutes = map[string]bool{
	"GET /openapi.json":       true,
	"GET /swagger":            true,
	"GET /swagger/":           true,
	"GET /swagger/index.html": true,
}

var httpMethods = map[string]bool{
	"get": true, "post": true, "put": true, "patch": true, "delete": true,
}

type openAPIDoc struct {
	Components struct {
		SecuritySchemes map[string]json.RawMessage `json:"securitySchemes"`
		Schemas         map[string]json.RawMessage `json:"schemas"`
	} `json:"components"`
	Paths map[string]map[string]json.RawMessage `json:"paths"`
}

type openAPIOperation struct {
	Parameters []struct {
		Name string `json:"name"`
		In   string `json:"in"`
	} `json:"parameters"`
}

// specFiles are the two OpenAPI documents, split along the listener boundary:
// the public API and the worker control plane. See
// docs/design/api-surface-conventions.md §5 and worker-api-network-boundary.md.
var specFiles = map[string]string{
	"public": "openapi.json",
	"worker": "openapi-worker.json",
}

func loadOpenAPI(t *testing.T, root, name string) openAPIDoc {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "internal", "server", "static", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	var doc openAPIDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("%s is not valid JSON: %v", name, err)
	}
	return doc
}

// documentedOps returns the "METHOD /path" set a document describes.
func documentedOps(doc openAPIDoc) map[string]bool {
	out := map[string]bool{}
	for path, item := range doc.Paths {
		for method := range item {
			if httpMethods[method] {
				out[strings.ToUpper(method)+" "+path] = true
			}
		}
	}
	return out
}

// registeredRoutes returns every "METHOD /pattern" the server registers, across
// both listeners.
//
// It scans every package under internal/server, not just handlers/routes.go:
// that file composes the subpackages and registers only a handful itself, so
// scanning it alone would miss most of the API. The variable is matched loosely
// -- mux, publicMux, workerMux -- because the server registers the public and
// worker route sets on two separate muxes, and this scan is the union of both.
func registeredRoutes(t *testing.T, root string) map[string]string {
	t.Helper()
	pattern := regexp.MustCompile(`\w*[Mm]ux\.Handle(?:Func)?\("([A-Z]+) ([^"]+)"`)
	routes := map[string]string{}
	serverDir := filepath.Join(root, "internal", "server")
	err := filepath.Walk(serverDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range pattern.FindAllStringSubmatch(string(src), -1) {
			rel, _ := filepath.Rel(root, path)
			routes[m[1]+" "+m[2]] = rel
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", serverDir, err)
	}
	if len(routes) == 0 {
		t.Fatal("found no route registrations; the scan or the registration style changed")
	}
	return routes
}

// openAPIPath converts a Go 1.22 route pattern to its OpenAPI path.
//
// The only difference is the wildcard: Go's {path...} matches the rest of the
// path, which OpenAPI 3 cannot express, so the document spells it {path} and
// the parameter's description carries the rest.
func openAPIPath(pattern string) string {
	return strings.ReplaceAll(pattern, "...}", "}")
}

// TestOpenAPICoversEveryRoute holds each document to an exact match with its
// listener's routes. The spec is split along the listener boundary, so the
// public document describes every public route and the worker document every
// worker route — and neither describes the other's. Both directions matter: an
// undocumented route leaves a caller with no contract, and a documented route
// the server does not register sends them somewhere that answers 404.
func TestOpenAPICoversEveryRoute(t *testing.T) {
	root := repoRoot(t)
	publicOps := documentedOps(loadOpenAPI(t, root, specFiles["public"]))
	workerOps := documentedOps(loadOpenAPI(t, root, specFiles["worker"]))

	registeredPublic := map[string]bool{}
	registeredWorker := map[string]bool{}
	for route, file := range registeredRoutes(t, root) {
		if deliveryRoutes[route] {
			continue
		}
		op := openAPIPath(route)
		if strings.HasPrefix(strings.SplitN(op, " ", 2)[1], workerRoutePrefix) {
			registeredWorker[op] = true
			if !workerOps[op] {
				t.Errorf("%s is registered in %s but openapi-worker.json does not describe it", route, file)
			}
			if publicOps[op] {
				t.Errorf("%s is a worker route but openapi.json (public) describes it; the worker plane must stay out of the public document", route)
			}
			continue
		}
		registeredPublic[op] = true
		if !publicOps[op] {
			t.Errorf("%s is registered in %s but openapi.json does not describe it", route, file)
		}
	}
	for op := range publicOps {
		if !registeredPublic[op] {
			t.Errorf("openapi.json describes %s, which the server does not register on the public listener", op)
		}
	}
	for op := range workerOps {
		if !registeredWorker[op] {
			t.Errorf("openapi-worker.json describes %s, which the server does not register on the worker listener", op)
		}
	}
}

// TestOpenAPIDeclaresPathParameters keeps path templating valid. A path that
// names {issue_id} without declaring it is not a contract a generator or a
// client can use.
func TestOpenAPIDeclaresPathParameters(t *testing.T) {
	root := repoRoot(t)
	for _, name := range specFiles {
		declaresPathParameters(t, loadOpenAPI(t, root, name), name)
	}
}

func declaresPathParameters(t *testing.T, doc openAPIDoc, name string) {
	t.Helper()
	template := regexp.MustCompile(`\{([^}]+)\}`)

	for path, item := range doc.Paths {
		var shared []string
		if raw, ok := item["parameters"]; ok {
			var params openAPIOperation
			if err := json.Unmarshal([]byte(`{"parameters":`+string(raw)+`}`), &params); err != nil {
				t.Fatalf("%s: path-level parameters: %v", path, err)
			}
			for _, p := range params.Parameters {
				if p.In == "path" {
					shared = append(shared, p.Name)
				}
			}
		}
		for method, raw := range item {
			if !httpMethods[method] {
				continue
			}
			var op openAPIOperation
			if err := json.Unmarshal(raw, &op); err != nil {
				t.Fatalf("%s %s: %v", method, path, err)
			}
			declared := map[string]bool{}
			for _, p := range shared {
				declared[p] = true
			}
			for _, p := range op.Parameters {
				if p.In == "path" {
					declared[p.Name] = true
				}
			}
			for _, m := range template.FindAllStringSubmatch(path, -1) {
				if !declared[m[1]] {
					t.Errorf("%s: %s %s templates {%s} but declares no such path parameter",
						name, strings.ToUpper(method), path, m[1])
				}
			}
		}
	}
}

// TestOpenAPITimestampsAreRFC3339 enforces the wire half of the timestamp
// convention on the document itself. A persisted instant is a time.Time in Go
// and an RFC 3339 string on the wire, so a schema calling one an integer
// describes a response the server has never sent.
//
// See docs/contribute/conventions.md and docs/design/timestamp-representation.md.
func TestOpenAPITimestampsAreRFC3339(t *testing.T) {
	root := repoRoot(t)
	for _, name := range specFiles {
		timestampsAreRFC3339(t, root, name)
	}
}

func timestampsAreRFC3339(t *testing.T, root, specName string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "internal", "server", "static", specName))
	if err != nil {
		t.Fatalf("read %s: %v", specName, err)
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("%s is not valid JSON: %v", specName, err)
	}
	// Instant-valued property names. A duration, a count, and a quota are not
	// instants and stay numbers, so the suffix is what selects them.
	instant := regexp.MustCompile(`(^|_)(at|time)$`)
	var walk func(node any, path string)
	walk = func(node any, path string) {
		switch v := node.(type) {
		case map[string]any:
			props, ok := v["properties"].(map[string]any)
			if ok {
				for name, schema := range props {
					s, ok := schema.(map[string]any)
					if !ok || !instant.MatchString(name) {
						continue
					}
					if s["type"] != "string" || s["format"] != "date-time" {
						t.Errorf("%s: %s.%s is %v/%v; an instant is a string with format date-time",
							specName, path, name, s["type"], s["format"])
					}
				}
			}
			for k, child := range v {
				walk(child, path+"/"+k)
			}
		case []any:
			for i, child := range v {
				walk(child, path+"/"+itoa(i))
			}
		}
	}
	walk(doc, "")
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
