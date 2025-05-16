package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/doc"
	"go/token"
	"go/types"
	"log"
	"os"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

type StructInfo struct {
	Name   string
	Doc    string
	Fields []FieldInfo
}

type FieldInfo struct {
	Name string
	Type string
	Tag  string
	Doc  string
	Typ  types.Type // ← add this field
}

type constEntry struct {
	Name  string
	Value string
	Doc   string
}

var visited = map[string]bool{}
var structMap = map[string]StructInfo{}
var rendered = map[string]bool{}
var constMap = map[string][]constEntry{}

var skipTypes = map[string]bool{
	"ProviderDealRejectionInfo": true,
	"DBDeal":                    true,
	"dbProduct":                 true,
	"dbDataSource":              true,
	"productAndDataSource":      true,
	"MK20":                      true,
	"DealStatus":                true,
}

var includeConsts = map[string]bool{
	"ErrorCode": true,
	"DealState": true,
}

func main() {
	var pkgPath, output string
	flag.StringVar(&pkgPath, "pkg", "./", "Package to scan")
	flag.StringVar(&output, "output", "info.md", "Output file")
	flag.Parse()

	cfg := &packages.Config{
		Mode: packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedDeps | packages.NeedImports | packages.NeedName | packages.NeedFiles,
		Fset: token.NewFileSet(),
	}

	pkgs, err := packages.Load(cfg, pkgPath)
	if err != nil {
		log.Fatalf("Failed to load package: %v", err)
	}

	for _, pkg := range pkgs {
		docPkg, err := doc.NewFromFiles(cfg.Fset, pkg.Syntax, pkg.PkgPath)
		if err != nil {
			log.Fatalf("Failed to parse package: %v", err)
		}
		for _, t := range docPkg.Types {
			if st, ok := t.Decl.Specs[0].(*ast.TypeSpec); ok {
				if structType, ok := st.Type.(*ast.StructType); ok {
					name := st.Name.Name
					if visited[name] || skipTypes[name] {
						continue
					}
					visited[name] = true
					collectStruct(pkg, name, structType, t.Doc)
				}
			}
		}
		for _, file := range pkg.Syntax {
			for _, decl := range file.Decls {
				genDecl, ok := decl.(*ast.GenDecl)
				if !ok || genDecl.Tok != token.CONST {
					continue
				}

				for _, spec := range genDecl.Specs {
					vspec := spec.(*ast.ValueSpec)
					for _, name := range vspec.Names {
						obj := pkg.TypesInfo.Defs[name]
						if obj == nil {
							continue
						}
						typ := obj.Type().String() // e.g., "main.ErrCode"
						parts := strings.Split(typ, ".")
						typeName := parts[len(parts)-1] // just "ErrCode"

						if !includeConsts[typeName] {
							continue
						}

						if !rendered[typeName] {
							constMap[typeName] = []constEntry{}
							rendered[typeName] = true
						}

						val := ""
						if con, ok := obj.(*types.Const); ok {
							val = con.Val().ExactString()
						}
						cdoc := strings.TrimSpace(vspec.Doc.Text())
						constMap[typeName] = append(constMap[typeName], constEntry{
							Name:  name.Name,
							Value: val,
							Doc:   cdoc,
						})
					}
				}
			}
		}
	}

	writeOutput(output)
}

func collectStruct(pkg *packages.Package, name string, structType *ast.StructType, docText string) {
	info := StructInfo{
		Name: name,
		Doc:  strings.TrimSpace(docText),
	}

	for _, field := range structType.Fields.List {
		var fieldName string
		if len(field.Names) > 0 {
			fieldName = field.Names[0].Name
		} else {
			fieldName = fmt.Sprintf("%s", field.Type)
		}

		var fieldType string
		//if typ := pkg.TypesInfo.TypeOf(field.Type); typ != nil {
		//	fieldType = types.TypeString(typ, func(p *types.Package) string {
		//		return p.Name()
		//	})
		//} else {
		//	fieldType = fmt.Sprintf("%s", field.Type)
		//}
		var typ types.Type
		if t := pkg.TypesInfo.TypeOf(field.Type); t != nil {
			typ = t
			fieldType = types.TypeString(t, func(p *types.Package) string {
				return p.Name()
			})
		}

		fieldTag := ""
		if field.Tag != nil {
			fieldTag = field.Tag.Value
		}

		var fieldDoc string
		if field.Doc != nil {
			lines := strings.Split(field.Doc.Text(), "\n")
			for i := range lines {
				lines[i] = strings.TrimSpace(lines[i])
			}
			fieldDoc = strings.Join(lines, " ")
		}

		info.Fields = append(info.Fields, FieldInfo{
			Name: fieldName,
			Type: fieldType,
			Tag:  fieldTag,
			Doc:  fieldDoc,
			Typ:  typ,
		})

		baseType := fieldType

		baseType = strings.TrimPrefix(baseType, "*")

		baseType = strings.TrimPrefix(baseType, "[]")
		baseType = strings.Split(baseType, ".")[0]
		if skipTypes[baseType] {
			continue
		}
		if !visited[baseType] {
			visited[baseType] = true
			collectFromImports(baseType)
		}
	}

	structMap[name] = info
}

func collectFromImports(typeName string) {
	// future: support nested imports with doc.New(...)
}

func writeOutput(path string) {
	var buf bytes.Buffer

	buf.WriteString("# Storage Market Interface\n\n")
	buf.WriteString("This document describes the storage market types and supported HTTP methods for making deals with Curio Storage Provider.\n\n")

	buf.WriteString("## \U0001F4E1 MK20 HTTP API Overview\n\n")
	buf.WriteString("The MK20 storage market module provides a set of HTTP endpoints under `/market/mk20` that allow clients to submit, track, and finalize storage deals with storage providers. This section documents all available routes and their expected behavior.\n\n")

	buf.WriteString("### Base URL\n\n" +
		"The base URL for all MK20 endpoints is: \n\n" +
		"```\n\n/market/mk20\n\n```" +
		"\n\n")

	buf.WriteString("### 🔄 POST /store\n\n")
	buf.WriteString("Submit a new MK20 deal.\n\n")
	buf.WriteString("- **Content-Type**: N/A\n")
	buf.WriteString("- **Body**: N/A\n")
	buf.WriteString("- **Query Parameters**: N/A\n")
	buf.WriteString("- **Response**:\n")
	buf.WriteString("  - `200 OK`: Deal accepted\n")
	buf.WriteString("  - Other [HTTP codes](#constants-for-errorcode) indicate validation failure, rejection, or system errors\n\n")

	buf.WriteString("### 🧾 GET /status?id=<ulid>\n\n")
	buf.WriteString("Retrieve the current status of a deal.\n\n")
	buf.WriteString("- **Content-Type**: `application/json`\n")
	buf.WriteString("- **Body**: N/A\n")
	buf.WriteString("- **Query Parameters**:\n")
	buf.WriteString("  - `id`: Deal identifier in [ULID](https://github.com/ulid/spec) format\n")
	buf.WriteString("- **Response**:\n")
	buf.WriteString("  - `200 OK`: JSON-encoded [deal status](#dealstatusresponse) information\n")
	buf.WriteString("  - `400 Bad Request`: Missing or invalid ID\n")
	buf.WriteString("  - `500 Internal Server Error`: If backend fails to respond\n\n")

	buf.WriteString("### 📜 GET /contracts\n\n")
	buf.WriteString("- **Content-Type**: N/A\n")
	buf.WriteString("- **Body**: N/A\n")
	buf.WriteString("- **Query Parameters**: N/A\n")
	buf.WriteString("Return the list of contract addresses supported by the provider.\n\n")
	buf.WriteString("- **Response**:\n")
	buf.WriteString("  - `200 OK`: [JSON array of contract addresses](#supportedcontracts)\n")
	buf.WriteString("  - `500 Internal Server Error`: Query or serialization failure\n\n")

	buf.WriteString("### 🗂 PUT /data?id=<ulid>\n\n")
	buf.WriteString("Upload deal data after the deal has been accepted.\n\n")
	buf.WriteString("- **Content-Type**: `application/octet-stream`\n")
	buf.WriteString("- **Body**: Deal data bytes\n")
	buf.WriteString("- **Query Parameter**:\n -`id`: Deal identifier in [ULID](https://github.com/ulid/spec) format\n")
	buf.WriteString("- **Headers**:\n")
	buf.WriteString("  - `Content-Length`: must be deal's raw size\n")
	buf.WriteString("- **Response**:\n")
	buf.WriteString("  - `200 OK`: if data is successfully streamed\n")
	buf.WriteString("  - `400`, `413`, or `415`: on validation failures\n\n")

	buf.WriteString("### 🧠 GET /info\n\n")
	buf.WriteString("- **Content-Type**: N/A\n")
	buf.WriteString("- **Body**: N/A\n")
	buf.WriteString("- **Query Parameters**: N/A\n")
	buf.WriteString("Fetch markdown-formatted documentation that describes the supported deal schema, products, and data sources.\n\n")
	buf.WriteString("- **Response**:\n")
	buf.WriteString("  - `200 OK`: with markdown content of the info file\n")
	buf.WriteString("  - `500 Internal Server Error`: if file is not found or cannot be read\n\n")

	buf.WriteString("## Supported Deal Types\n\n")
	buf.WriteString("This document lists the data types and fields supported in the new storage market interface. It defines the deal structure, accepted data sources, and optional product extensions. Clients should use these definitions to format and validate their deals before submission.\n\n")

	ordered := []string{"Deal", "DataSource", "Products"}
	var rest []string
	for k := range structMap {
		if k != "Deal" && k != "DataSource" && k != "Products" {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	keys := append(ordered, rest...)

	for _, k := range keys {
		s, ok := structMap[k]
		if !ok {
			continue
		}
		buf.WriteString(fmt.Sprintf("### %s\n\n", s.Name))
		if s.Doc != "" {
			buf.WriteString(s.Doc + "\n\n")
		}
		buf.WriteString("| Field | Type | Tag | Description |\n")
		buf.WriteString("|-------|------|-----|-------------|\n")
		for _, f := range s.Fields {
			typeName := f.Type
			linkTarget := ""

			// Strip common wrappers like pointer/star and slice
			trimmed := strings.TrimPrefix(typeName, "*")
			trimmed = strings.TrimPrefix(trimmed, "[]")
			parts := strings.Split(trimmed, ".")
			baseType := parts[len(parts)-1]

			if _, ok := structMap[baseType]; ok {
				linkTarget = fmt.Sprintf("[%s](#%s)", f.Type, strings.ToLower(baseType))
			} else if _, ok := constMap[baseType]; ok {
				linkTarget = fmt.Sprintf("[%s](#constants-for-%s)", f.Type, strings.ToLower(baseType))
			} else {
				typ := f.Typ
				if ptr, ok := typ.(*types.Pointer); ok {
					typ = ptr.Elem()
				}
				if named, ok := typ.(*types.Named); ok && named.Obj() != nil && named.Obj().Pkg() != nil {

					pkgPath := named.Obj().Pkg().Path()
					objName := named.Obj().Name()
					linkTarget = fmt.Sprintf("[%s](https://pkg.go.dev/%s#%s)", typeName, pkgPath, objName)
				} else if typ != nil && typ.String() == baseType {
					linkTarget = fmt.Sprintf("[%s](https://pkg.go.dev/builtin#%s)", typeName, baseType)
				} else if slice, ok := typ.(*types.Slice); ok {
					elem := slice.Elem()
					if basic, ok := elem.(*types.Basic); ok {
						linkTarget = fmt.Sprintf("[%s](https://pkg.go.dev/builtin#%s)", typeName, basic.Name())
					} else {
						linkTarget = typeName
					}
				} else {
					linkTarget = typeName
				}
			}

			buf.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n",
				f.Name, linkTarget, strings.Trim(f.Tag, "`"), f.Doc))
		}
		buf.WriteString("\n")
	}

	// Render constants with sort order
	for k, v := range constMap {
		if len(v) == 0 {
			continue
		}
		buf.WriteString(fmt.Sprintf("### Constants for %s\n\n", k))
		buf.WriteString("| Constant | Code | Description |\n")
		buf.WriteString("|----------|------|-------------|\n")
		for _, c := range v {
			buf.WriteString(fmt.Sprintf("| %s | %s | %s |\n", c.Name, c.Value, c.Doc))
		}
		buf.WriteString("\n")
	}

	err := os.WriteFile(path, buf.Bytes(), 0644)
	if err != nil {
		log.Fatalf("Failed to write output: %v", err)
	}
}
