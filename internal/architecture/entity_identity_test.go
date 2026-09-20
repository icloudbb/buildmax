package architecture_test

import (
	"go/ast"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// The rules in this file are the schema half of
// docs/design/entity-identity.md. A server entity has two identifiers with one
// role each: a bigint row key that never leaves internal/infra/db, and an
// opaque public_id that is the only handle any boundary sees.
//
// They are parsed from source rather than asserted against a live database
// because the row structs are the schema: a rule that needs MySQL to run is a
// rule that does not run in CI.

// opaqueColumns are the *_id columns that stay text on purpose. Each is listed
// with the reason one numeric reference cannot say what it says, because that
// reason is the whole test: a column added here without one is how the rule
// erodes.
var opaqueColumns = map[string]string{
	// Polymorphic: a type column beside them names which table, or names
	// something that is not a table at all.
	"audit_event.actor_id":         "actor_type admits an operator and a worker, neither a user row",
	"audit_event.target_id":        "target_type admits a permission name and a model alias",
	"audit_event.task_run_id":      "an opaque run handle by design: the trail does not take a foreign key into the execution plane, whose rows have their own retention, so an investigation reaches the run without a join and a pruned run does not break the evidence",
	"issue.executor_id":            "executor_kind admits agent or workflow",
	"schedule.executor_id":         "executor_kind admits agent or workflow, mirroring issue.executor_id",
	"issue_comment.author_id":      "author_kind admits an agent as well as a person",
	"artifact.created_by_id":       "created_by_type admits a worker",
	"artifact.source_id":           "source_type admits a run, a conversation, or an upload",
	"artifact_share.created_by_id": "created_by_type admits a worker, mirroring artifact.created_by_id",
	"task_run.created_by":          "created_by_type admits webhook and system",
	"system_grant.granted_by":      "the operator who bootstraps the first grant is a command line",
	"task_run.cancel_requested_by": "a cancel can be recorded for a run whose canceller is not a user row",

	// Externally owned: the format belongs to something other than this schema.
	"conversation_message.tool_call_id": "a provider's tool-call id",
	"llm_call.session_id":               "an agent session names a file under a run's BUILDMAX_HOME",
	"llm_call.target_id":                "a catalog id whose namespace may belong to configuration",
	"task.session_id":                   "an agent session names a file",
	"task_run.session_id":               "an agent session names a file",
	"user_refresh_token.session_id":     "a login chain, carried as a claim in every access token issued under it",
	"login_code.code_hash":              "an authentication format",
	"user_refresh_token.token_hash":     "an authentication format",
	"user_webhook_key.key_hash":         "an authentication format",
	"user_refresh_token.replaced_by":    "the hash of the next token in the chain, not a reference to a row",
	"secret.key_id":                     "names the KEK that wrapped this row's DEK, a <backend>:<name>:<version> string, not a reference to a row",
}

// numericRelationExempt are columns whose name ends in _id but which are not
// references at all.
var numericRelationExempt = map[string]bool{
	"workflow_node_run.node_id": true, // authored inside a workflow definition
	"schema_migration.id":       true, // the migration's permanent authored name
}

type rowField struct {
	file   string
	table  string
	column string
	tag    reflect.StructTag
	goType string
}

// TestEntityRelationshipsAreNumeric fails when a reference is stored as text.
//
// cancel_requested_by is the shape this catches: it was a varchar naming a
// user, and nothing but a rule stops the next one being written the same way.
func TestEntityRelationshipsAreNumeric(t *testing.T) {
	// A _type column beside a reference says which table it names. It is a
	// discriminator, never a reference itself.
	isReference := func(col string) bool {
		if strings.HasSuffix(col, "_type") {
			return false
		}
		return strings.HasSuffix(col, "_id") || strings.HasSuffix(col, "_by")
	}
	for _, f := range rowFields(t) {
		if !isReference(f.column) {
			continue
		}
		qualified := f.table + "." + f.column
		if numericRelationExempt[qualified] || opaqueColumns[qualified] != "" {
			continue
		}
		if f.column == "public_id" {
			continue
		}
		if strings.Contains(string(f.tag), "varchar") || strings.Contains(f.goType, "string") {
			t.Errorf("%s: %s is text; a reference to one entity is a bigint, "+
				"or it belongs in opaqueColumns with the reason it cannot be",
				f.file, qualified)
		}
	}
}

// TestNoDatabaseForeignKeys fails when a row struct declares a GORM relation.
//
// docs/design/entity-identity.md §8 decided this rather than deferring it
// again: no hard delete in the store removes a referenced parent, so RESTRICT
// would guard a hazard that does not exist and charge every fixture a teardown
// in dependency order for it. The constraints arrive with the first real
// deletion feature, where that order has to be written down anyway.
//
// Numeric references are what make the rule worth a test. A bigint column
// beside a row struct is one tag away from a constraint AutoMigrate will
// happily emit, and nothing else in the tree would notice it had appeared.
func TestNoDatabaseForeignKeys(t *testing.T) {
	root := moduleRoot(t)
	var paths []string
	for _, path := range goFiles(t, filepath.Join(root, "internal/infra/db")) {
		if !strings.HasSuffix(path, "_test.go") {
			paths = append(paths, path)
		}
	}
	files := make([]*ast.File, 0, len(paths))
	rowStructs := map[string]bool{}
	for _, path := range paths {
		file := parseFile(t, path)
		files = append(files, file)
		for name := range tableNames(file) {
			rowStructs[name] = true
		}
	}
	if len(rowStructs) == 0 {
		t.Fatal("found no row structs; the parser stopped matching TableName methods")
	}

	// The tag keys that declare an association, and so the ones AutoMigrate
	// reads before emitting a FOREIGN KEY.
	relationTags := map[string]bool{
		"foreignkey":  true,
		"references":  true,
		"many2many":   true,
		"constraint":  true,
		"polymorphic": true,
	}

	for i, file := range files {
		path := rel(root, paths[i])
		tables := tableNames(file)
		ast.Inspect(file, func(n ast.Node) bool {
			spec, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			st, ok := spec.Type.(*ast.StructType)
			if !ok || tables[spec.Name.Name] == "" {
				return true
			}
			for _, field := range st.Fields.List {
				name := spec.Name.Name
				if len(field.Names) > 0 {
					name += "." + field.Names[0].Name
				}
				if field.Tag != nil {
					raw, err := strconv.Unquote(field.Tag.Value)
					if err != nil {
						t.Fatalf("unquote tag in %s: %v", path, err)
					}
					for _, part := range strings.Split(reflect.StructTag(raw).Get("gorm"), ";") {
						key, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(part)), ":")
						if relationTags[key] {
							t.Errorf("%s: %s declares gorm %q; a reference is a plain "+
								"indexed column and AutoMigrate must emit no FOREIGN KEY",
								path, name, key)
						}
					}
				}
				if base := baseTypeName(exprString(field.Type)); rowStructs[base] {
					t.Errorf("%s: %s is typed %s, another row struct; GORM infers an "+
						"association from that and AutoMigrate emits its FOREIGN KEY",
						path, name, base)
				}
			}
			return true
		})
	}
}

// baseTypeName strips pointers and slices from a type expression.
func baseTypeName(s string) string {
	for {
		switch {
		case strings.HasPrefix(s, "*"):
			s = s[1:]
		case strings.HasPrefix(s, "[]"):
			s = s[2:]
		default:
			return s
		}
	}
}

// TestPublicIDsAreCanonicalText fails when a handle is stored as anything but
// char(20) ascii_bin.
//
// The column stores the canonical text form so a direct database query reads
// the same handle every API response shows — the raw-byte form made every
// public_id an unreadable blob, and that operational cost bought 8 bytes per
// value. ascii_bin keeps identity out of collation's hands: comparison is
// memcmp, exactly as it was over binary(12), because the store writes and
// queries only the lowercase canonical form. A utf8mb4 or _ci public_id would
// quietly hand "which handles are the same" back to the database.
func TestPublicIDsAreCanonicalText(t *testing.T) {
	seen := 0
	for _, f := range rowFields(t) {
		if f.column != "public_id" {
			continue
		}
		seen++
		if !strings.Contains(string(f.tag), "type:char(20) CHARACTER SET ascii COLLATE ascii_bin") {
			t.Errorf("%s: %s.public_id is not char(20) ascii_bin", f.file, f.table)
		}
		if f.goType != "string" {
			t.Errorf("%s: %s.public_id is %s, want string", f.file, f.table, f.goType)
		}
		want := "uniqueIndex:uq_" + f.table + "_public_id"
		if !strings.Contains(string(f.tag), want) {
			t.Errorf("%s: %s.public_id lacks %q; the unique index is the collision guard, "+
				"and its name is what tells a collision from a duplicate email",
				f.file, f.table, want)
		}
	}
	if seen == 0 {
		t.Fatal("found no public_id columns; the parser stopped seeing the row structs")
	}
}

// TestRowKeysStayInsideTheStore fails when a row key can be named from outside
// internal/infra/db.
//
// The keys are unexported fields of unexported types, so the only way one
// escapes is through an exported signature. That is the whole surface, and it
// is worth a rule: an exported helper taking a key would put a MySQL detail in
// every caller and every mock.
func TestRowKeysStayInsideTheStore(t *testing.T) {
	root := moduleRoot(t)
	for _, path := range goFiles(t, filepath.Join(root, "internal/infra/db")) {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f := parseFile(t, path)
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !fn.Name.IsExported() {
				continue
			}
			for _, field := range signatureTypes(fn) {
				if field == "uint64" || field == "*uint64" {
					t.Errorf("%s: exported %s has a row key in its signature; "+
						"a caller outside this package must never hold one",
						rel(root, path), fn.Name.Name)
				}
			}
		}
	}
}

func signatureTypes(fn *ast.FuncDecl) []string {
	var out []string
	collect := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, f := range fl.List {
			out = append(out, exprString(f.Type))
		}
	}
	collect(fn.Type.Params)
	collect(fn.Type.Results)
	return out
}

// rowFields returns every struct field in internal/infra/db that carries a
// gorm tag, with the table its struct names.
func rowFields(t *testing.T) []rowField {
	t.Helper()
	root := moduleRoot(t)
	var out []rowField
	for _, path := range goFiles(t, filepath.Join(root, "internal/infra/db")) {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file := parseFile(t, path)
		tables := tableNames(file)
		ast.Inspect(file, func(n ast.Node) bool {
			spec, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			st, ok := spec.Type.(*ast.StructType)
			if !ok {
				return true
			}
			table := tables[spec.Name.Name]
			if table == "" {
				return true
			}
			for _, field := range st.Fields.List {
				if field.Tag == nil || len(field.Names) == 0 {
					continue
				}
				raw, err := strconv.Unquote(field.Tag.Value)
				if err != nil {
					t.Fatalf("unquote tag in %s: %v", path, err)
				}
				tag := reflect.StructTag(raw)
				out = append(out, rowField{
					file:   rel(root, path),
					table:  table,
					column: columnName(tag, field.Names[0].Name),
					tag:    tag,
					goType: exprString(field.Type),
				})
			}
			return true
		})
	}
	if len(out) == 0 {
		t.Fatal("found no row struct fields; the parser stopped matching TableName methods")
	}
	return out
}

// tableNames maps a row struct's Go name to the table its TableName reports.
// A struct without one is not a table and is skipped.
func tableNames(file *ast.File) map[string]string {
	out := map[string]string{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "TableName" || fn.Recv == nil || len(fn.Recv.List) != 1 {
			continue
		}
		recv := strings.TrimPrefix(exprString(fn.Recv.List[0].Type), "*")
		ast.Inspect(fn, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok {
				return true
			}
			if name, err := strconv.Unquote(lit.Value); err == nil {
				out[recv] = name
			}
			return true
		})
	}
	return out
}

// columnName is the column a field maps to: the gorm tag's column: when it has
// one, and GORM's snake_case of the field name otherwise.
func columnName(tag reflect.StructTag, fieldName string) string {
	for _, part := range strings.Split(tag.Get("gorm"), ";") {
		if after, found := strings.CutPrefix(part, "column:"); found {
			return after
		}
	}
	return snakeCase(fieldName)
}

func snakeCase(s string) string {
	var b strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r - 'A' + 'a')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func exprString(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + exprString(t.X)
	case *ast.ArrayType:
		return "[]" + exprString(t.Elt)
	case *ast.SelectorExpr:
		return exprString(t.X) + "." + t.Sel.Name
	case *ast.Ellipsis:
		return "..." + exprString(t.Elt)
	}
	return ""
}

// TestModelIdentifiersCrossAsStrings fails when a domain struct carries an
// identifier as a number.
//
// internal/core is what every boundary serializes: an API response, a JWT
// claim, an object key, a trace record, a WebSocket frame. Keeping row keys out
// of it is what keeps them out of all of those at once, and it is why the
// models lost their ID uint rather than hiding it behind json:"-" -- a field
// that exists is a field a log line or a mock can reach.
//
// The whole tree is scanned, not one package: a domain moving into its own
// package must not move out of this rule with it.
func TestModelIdentifiersCrossAsStrings(t *testing.T) {
	root := moduleRoot(t)
	// Numeric fields whose name ends in ID but which count something rather
	// than name it.
	numericByDesign := map[string]bool{
		"AuditCursor.ID": false, // a keyset position, and it is the public handle
	}
	checked := 0
	for _, path := range goFiles(t, filepath.Join(root, "internal/core")) {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file := parseFile(t, path)
		ast.Inspect(file, func(n ast.Node) bool {
			spec, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			st, ok := spec.Type.(*ast.StructType)
			if !ok {
				return true
			}
			for _, field := range st.Fields.List {
				for _, name := range field.Names {
					if !strings.HasSuffix(name.Name, "ID") && name.Name != "ID" {
						continue
					}
					checked++
					qualified := spec.Name.Name + "." + name.Name
					if numericByDesign[qualified] {
						continue
					}
					goType := exprString(field.Type)
					if goType != "string" && goType != "*string" {
						t.Errorf("%s: %s is %s; an identifier crosses this boundary as text",
							rel(root, path), qualified, goType)
					}
				}
			}
			return true
		})
	}
	if checked == 0 {
		t.Fatal("found no identifier fields in internal/core")
	}
}
