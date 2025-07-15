package main

//
//import (
//	"bytes"
//	"flag"
//	"fmt"
//	"go/ast"
//	"go/doc"
//	"go/token"
//	"go/types"
//	"log"
//	"os"
//	"sort"
//	"strings"
//
//	"golang.org/x/tools/go/packages"
//)
//
//// Note: This file has too many static things. Go parse package is not easy to work with and
//// is a nightmare. Wasting month[s] to build a correct parses does not seem correct use of time.
//
//type StructInfo struct {
//	Name   string
//	Doc    string
//	Fields []*FieldInfo
//}
//
//type FieldInfo struct {
//	Name string
//	Type string
//	Tag  string
//	Doc  string
//	Typ  types.Type
//}
//
//type constEntry struct {
//	Name  string
//	Value string
//	Doc   string
//}
//
//var visited = map[string]bool{}
//var structMap = map[string]*StructInfo{}
//var rendered = map[string]bool{}
//var constMap = map[string][]constEntry{}
//
//var skipTypes = map[string]bool{
//	"ProviderDealRejectionInfo": true,
//	"DBDeal":                    true,
//	"dbProduct":                 true,
//	"dbDataSource":              true,
//	"productAndDataSource":      true,
//	"MK20":                      true,
//	"DealStatus":                true,
//}
//
//var includeConsts = map[string]bool{
//	"DealCode":         true,
//	"DealState":        true,
//	"UploadStatusCode": true,
//	"UploadStartCode":  true,
//	"UploadCode":       true,
//}
//
////type ParamDoc struct {
////	Name     string
////	Type     string
////	Optional bool
////	Comment  string
////}
////type ReturnDoc struct {
////	Name    string
////	Type    string
////	Comment string
////}
//
////// FunctionDoc holds extracted param and return comments for a function.
////type FunctionDoc struct {
////	Params  []ParamDoc
////	Returns []ReturnDoc
////}
////
////type handlerInfo struct {
////	Path             string
////	Method           string
////	FuncName         string
////	Calls            map[string]bool
////	Types            map[string]bool
////	Constants        map[string]bool
////	Errors           map[string]bool
////	RequestBodyType  string
////	ResponseBodyType string
////}
////
////var allHandlers = map[string]*handlerInfo{} // key = function name
////
////var httpCodes = map[string]struct {
////	Code string
////	Msg  string
////}{
////	"http.StatusBadRequest": {
////		Code: "400",
////		Msg:  "Bad Request - Invalid input or validation error",
////	},
////	"http.StatusOK": {
////		Code: "200",
////		Msg:  "OK - Success",
////	},
////	"http.StatusInternalServerError": {
////		Code: "500",
////		Msg:  "Internal Server Error",
////	},
////}
////
////var (
////	paramRe  = regexp.MustCompile(`@param\s+(\w+)\s+([^\s\[]+)(\s+\[optional\])?(.*)`)
////	returnRe = regexp.MustCompile(`@Return\s+(\w+)?\s*([^\s\[]+)?(.*)`)
////)
//
////func ParseFunctionDocsFromComments(pkgPath string) map[string]*FunctionDoc {
////	fset := token.NewFileSet()
////	pkgs, err := parser.ParseDir(fset, pkgPath, nil, parser.ParseComments)
////	if err != nil {
////		panic(err)
////	}
////
////	funcDocs := map[string]*FunctionDoc{}
////
////	for _, pkg := range pkgs {
////		for _, file := range pkg.Files {
////			for _, decl := range file.Decls {
////				fn, ok := decl.(*ast.FuncDecl)
////				if !ok || fn.Doc == nil {
////					continue
////				}
////
////				doc := &FunctionDoc{}
////				for _, c := range fn.Doc.List {
////					txt := strings.TrimSpace(strings.TrimPrefix(c.Text, "//"))
////					if m := paramRe.FindStringSubmatch(txt); m != nil {
////						doc.Params = append(doc.Params, ParamDoc{
////							Name:     m[1],
////							Type:     m[2],
////							Optional: strings.Contains(m[3], "optional"),
////							Comment:  strings.TrimSpace(m[4]),
////						})
////					} else if m := returnRe.FindStringSubmatch(txt); m != nil {
////						doc.Returns = append(doc.Returns, ReturnDoc{
////							Name:    m[1],
////							Type:    m[2],
////							Comment: strings.TrimSpace(m[3]),
////						})
////					}
////				}
////
////				if len(doc.Params) > 0 || len(doc.Returns) > 0 {
////					funcDocs[fn.Name.Name] = doc
////				}
////			}
////		}
////	}
////	return funcDocs
////}
//
//func main() {
//	var pkgPath, output string
//	flag.StringVar(&pkgPath, "pkg", "./", "Package to scan")
//	flag.StringVar(&output, "output", "info.md", "Output file")
//	flag.Parse()
//
//	//pkgPath := "/Users/lexluthr/github/filecoin-project/curio/market/mk20"
//	//routerFile := filepath.Join(pkgPath, "http", "http.go")
//
//	cfg := &packages.Config{
//		Mode: packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedDeps | packages.NeedImports | packages.NeedName | packages.NeedFiles | packages.LoadAllSyntax,
//		Fset: token.NewFileSet(),
//	}
//
//	pkgs, err := packages.Load(cfg, pkgPath)
//	if err != nil {
//		log.Fatalf("Failed to load package: %v", err)
//	}
//
//	for _, pkg := range pkgs {
//		docPkg, err := doc.NewFromFiles(cfg.Fset, pkg.Syntax, pkg.PkgPath)
//		if err != nil {
//			log.Fatalf("Failed to parse package: %v", err)
//		}
//		for _, t := range docPkg.Types {
//			if st, ok := t.Decl.Specs[0].(*ast.TypeSpec); ok {
//				if structType, ok := st.Type.(*ast.StructType); ok {
//					name := st.Name.Name
//					if visited[name] || skipTypes[name] {
//						continue
//					}
//					visited[name] = true
//					collectStruct(pkg, name, structType, t.Doc)
//				}
//			}
//		}
//		for _, file := range pkg.Syntax {
//			for _, decl := range file.Decls {
//				genDecl, ok := decl.(*ast.GenDecl)
//				if !ok || genDecl.Tok != token.CONST {
//					continue
//				}
//
//				for _, spec := range genDecl.Specs {
//					vspec := spec.(*ast.ValueSpec)
//					for _, name := range vspec.Names {
//						obj := pkg.TypesInfo.Defs[name]
//						if obj == nil {
//							continue
//						}
//						typ := obj.Type().String() // e.g., "main.ErrCode"
//						parts := strings.Split(typ, ".")
//						typeName := parts[len(parts)-1] // just "ErrCode"
//
//						if !includeConsts[typeName] {
//							continue
//						}
//
//						if !rendered[typeName] {
//							constMap[typeName] = []constEntry{}
//							rendered[typeName] = true
//						}
//
//						val := ""
//						if con, ok := obj.(*types.Const); ok {
//							val = con.Val().ExactString()
//						}
//						cdoc := strings.TrimSpace(vspec.Doc.Text())
//						constMap[typeName] = append(constMap[typeName], constEntry{
//							Name:  name.Name,
//							Value: val,
//							Doc:   cdoc,
//						})
//					}
//				}
//			}
//		}
//	}
//
//	//fm := ParseFunctionDocsFromComments(pkgPath)
//	//for fname, doc := range fm {
//	//	fmt.Printf("Function: %s\n", fname)
//	//	if len(doc.Params) > 0 {
//	//		fmt.Println("  Params:")
//	//		for _, p := range doc.Params {
//	//			fmt.Printf("    - %s %s", p.Name, p.Type)
//	//			if p.Optional {
//	//				fmt.Print(" (optional)")
//	//			}
//	//			if p.Comment != "" {
//	//				fmt.Printf(" -- %s", p.Comment)
//	//			}
//	//			fmt.Println()
//	//		}
//	//	}
//	//	if len(doc.Returns) > 0 {
//	//		fmt.Println("  Returns:")
//	//		for _, r := range doc.Returns {
//	//			fmt.Printf("    - Name: %s Type: %s", r.Name, r.Type)
//	//			if r.Comment != "" {
//	//				fmt.Printf(" -- Comment: %s", r.Comment)
//	//			}
//	//			fmt.Println()
//	//		}
//	//	}
//	//}
//
//	writeOutput(output)
//	//parseMux(routerFile)
//	//fmt.Println("Done tracing handlers")
//	//parseHandlerBodies(routerFile)
//	//fmt.Println("Done parsing handler bodies")
//	//for k, v := range allHandlers {
//	//	fmt.Println("------------------")
//	//	fmt.Println("Name:", k)
//	//	fmt.Println("Path:", v.Path)
//	//	fmt.Println("Method:", v.Method)
//	//	fmt.Println("Constants", v.Constants)
//	//	fmt.Println("Calls:", v.Calls)
//	//	fmt.Println("Types:", v.Types)
//	//	fmt.Println("RequestBody", v.RequestBodyType)
//	//	fmt.Println("ResponseBody", v.ResponseBodyType)
//	//	fmt.Println("------------------")
//	//}
//	//fmt.Println("----------------")
//	//fmt.Println("----------------")
//	//for k, v := range constMap {
//	//	fmt.Println("Name:", k)
//	//	for _, e := range v {
//	//		fmt.Printf("  - %s: %s\n", e.Name, e.Value)
//	//	}
//	//}
//	//fmt.Println("----------------")
//	//fmt.Println("----------------")
//	//for _, h := range allHandlers {
//	//	fmt.Printf("%s %s\n", h.Method, h.Path)
//	//	// Optional: print summary from docs if available
//	//	// Parameters
//	//	mainCall := ""
//	//	for call := range h.Calls {
//	//		if strings.HasPrefix(call, "mk20.") {
//	//			mainCall = strings.TrimPrefix(call, "mk20.")
//	//			break
//	//		}
//	//	}
//	//	if mainCall != "" {
//	//		fmt.Println("### Parameters")
//	//		if doc, ok := fm[mainCall]; ok {
//	//			for _, param := range doc.Params {
//	//				fmt.Printf("- %s (%s)%s\n", param.Name, param.Type,
//	//					func() string {
//	//						if param.Optional {
//	//							return " [optional]"
//	//						}
//	//						return ""
//	//					}())
//	//			}
//	//		} else if len(h.Types) > 0 {
//	//			// fallback: print type
//	//			for typ := range h.Types {
//	//				fmt.Printf("- body (%s)\n", typ)
//	//			}
//	//		}
//	//	}
//	//	// Responses
//	//	fmt.Println("### Possible Responses")
//	//	for code := range h.Constants {
//	//		switch code {
//	//		case "http.StatusBadRequest":
//	//			fmt.Println("- 400 Bad Request: Invalid input or validation error.")
//	//		case "http.StatusOK":
//	//			fmt.Println("- 200 OK: Success.")
//	//		case "http.StatusInternalServerError":
//	//			fmt.Println("- 500 Internal Server Error.")
//	//		// ... add more as needed
//	//		default:
//	//			fmt.Printf("- %s\n", code)
//	//		}
//	//	}
//	//	fmt.Println()
//	//}
//
//	//formatHandlerDocs(fm)
//	//generateSwaggoComments(fm)
//
//}
//
////func extractPathParams(path string) []string {
////	var out []string
////	for _, part := range strings.Split(path, "/") {
////		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
////			out = append(out, strings.TrimSuffix(strings.TrimPrefix(part, "{"), "}"))
////		}
////	}
////	return out
////}
//
////func generateSwaggoComments(funcDocs map[string]*FunctionDoc) {
////	for _, h := range allHandlers {
////		fmt.Printf("// @Router %s [%s]\n", h.Path, strings.ToLower(h.Method))
////
////		// Path parameters from {id}, {chunkNum}, etc.
////		for _, param := range extractPathParams(h.Path) {
////			fmt.Printf("// @Param %s path string true \"%s\"\n", param, param)
////		}
////
////		// Request body
////		if h.RequestBodyType != "" {
////			fmt.Println("// @accepts json")
////			fmt.Printf("// @Param body body %s true\n", h.RequestBodyType)
////			fmt.Println("// @Accept json\n// @Produce json")
////		} else if h.Method == "PUT" {
////			fmt.Println("// @accepts bytes")
////			fmt.Printf("// @Param body body []byte true \"raw binary\"\n")
////		}
////
////		// Figure out function called like mk20.Something
////		var mk20Call string
////		for call := range h.Calls {
////			if strings.HasPrefix(call, "mk20.") {
////				mk20Call = strings.TrimPrefix(call, "mk20.")
////				break
////			}
////		}
////
////		// Return codes (Swagger `@Success` / `@Failure`)
////		hasReturn := false
////		if doc, ok := funcDocs[mk20Call]; ok {
////			for _, ret := range doc.Returns {
////				key := strings.TrimPrefix(ret.Name, "*")
////				key = strings.TrimPrefix(key, "mk20.")
////				if entries, ok := constMap[key]; ok {
////					for _, entry := range entries {
////						msg := strings.TrimSuffix(entry.Doc, ".")
////						tag := "@Failure"
////						if strings.HasPrefix(fmt.Sprintf("%d", entry.Value), "2") {
////							tag = "@Success"
////							hasReturn = true
////						} else {
////							fmt.Printf("// %s %s {object} %s \"%s\"\n", tag, entry.Value, key, msg)
////						}
////					}
////				}
////			}
////			// Fallback to direct http constants if nothing above
////			for k := range h.Constants {
////				if msg, ok := httpCodes[k]; ok {
////					tag := "@Failure"
////					if strings.HasPrefix(fmt.Sprintf("%d", msg.Code), "2") {
////						tag = "@Success"
////						hasReturn = true
////					} else {
////						fmt.Printf("// %s %s {string} string \"%s\"\n", tag, msg.Code, msg.Msg)
////					}
////
////				}
////			}
////
////			// If known response type
////			if h.ResponseBodyType != "" && hasReturn {
////				fmt.Println("// @produce json")
////				fmt.Printf("// @Success 200 {object} %s\n", h.ResponseBodyType)
////			}
////		} else {
////			// Fallback to direct http constants if nothing above
////			for k := range h.Constants {
////				if msg, ok := httpCodes[k]; ok {
////					tag := "@Failure"
////					if strings.HasPrefix(fmt.Sprintf("%d", msg.Code), "2") {
////						tag = "@Success"
////						hasReturn = true
////					} else {
////						fmt.Printf("// %s %s {string} string \"%s\"\n", tag, msg.Code, msg.Msg)
////					}
////					//fmt.Printf("// %s %s {string} string \"%s\"\n", tag, msg.Code, msg.Msg)
////				}
////			}
////
////			// If known response type
////			if h.ResponseBodyType != "" && hasReturn {
////				fmt.Println("// @produce json")
////				fmt.Printf("// @Success 200 {object} %s\n", h.ResponseBodyType)
////			}
////		}
////
////		fmt.Println()
////	}
////}
////
////func formatHandlerDocs(funcDocs map[string]*FunctionDoc) {
////	for _, h := range allHandlers {
////		fmt.Printf("%s %s\n", h.Method, h.Path)
////
////		// 1. Find the mk20 call
////		var mk20Call string
////		for call := range h.Calls {
////			if strings.HasPrefix(call, "mk20.") {
////				mk20Call = strings.TrimPrefix(call, "mk20.")
////				//fmt.Println("mk20Call: ", mk20Call)
////				break
////			}
////		}
////
////		// 2. Look up params and returns
////		doc, ok := funcDocs[mk20Call]
////		if ok {
////			if h.RequestBodyType != "" {
////				fmt.Printf("### Request Body\n- %s\n", h.RequestBodyType)
////			}
////			if h.RequestBodyType == "" && h.Method == "PUT" {
////				fmt.Printf("### Request Body\n- bytes\n")
////			}
////
////			// 3. Lookup constMap based on return types
////			fmt.Println("### Possible Responses")
////			for _, ret := range doc.Returns {
////				key := strings.TrimPrefix(ret.Name, "*")
////				key = strings.TrimPrefix(key, "mk20.")
////				if entries, ok := constMap[key]; ok {
////					for _, entry := range entries {
////						comment := entry.Doc
////						comment = strings.TrimSuffix(comment, ".")
////						if comment == "" {
////							fmt.Printf("- %s: %s\n", entry.Value, entry.Name)
////						} else {
////							fmt.Printf("- %s: %s - %s\n", entry.Value, entry.Name, comment)
////						}
////					}
////				}
////			}
////			for k, _ := range h.Constants {
////				fmt.Printf("- %s\n", httpCodes[k])
////			}
////			if h.ResponseBodyType != "" {
////				fmt.Printf("### Response Body\n- %s\n", h.ResponseBodyType)
////			}
////		} else {
////			//fmt.Println("### Parameters")
////			if h.RequestBodyType != "" {
////				fmt.Printf("### Request Body\n- %s\n", h.RequestBodyType)
////			}
////			if h.RequestBodyType == "" && h.Method == "PUT" {
////				fmt.Printf("### Request Body\n- bytes\n")
////			}
////			fmt.Println("### Possible Responses")
////			for k, _ := range h.Constants {
////				fmt.Printf("- %s\n", httpCodes[k])
////			}
////			if h.ResponseBodyType != "" {
////				fmt.Printf("### Response Body\n- %s\n", h.ResponseBodyType)
////			}
////		}
////		fmt.Println()
////	}
////}
//
//func collectStruct(pkg *packages.Package, name string, structType *ast.StructType, docText string) {
//	info := StructInfo{
//		Name: name,
//		Doc:  strings.TrimSpace(docText),
//	}
//
//	for _, field := range structType.Fields.List {
//		var fieldName string
//		if len(field.Names) > 0 {
//			fieldName = field.Names[0].Name
//		} else {
//			fieldName = fmt.Sprintf("%s", field.Type)
//		}
//
//		var fieldType string
//		var typ types.Type
//		if t := pkg.TypesInfo.TypeOf(field.Type); t != nil {
//			typ = t
//			fieldType = types.TypeString(t, func(p *types.Package) string {
//				return p.Name()
//			})
//		}
//
//		fieldTag := ""
//		if field.Tag != nil {
//			fieldTag = field.Tag.Value
//		}
//
//		var fieldDoc string
//		if field.Doc != nil {
//			lines := strings.Split(field.Doc.Text(), "\n")
//			for i := range lines {
//				lines[i] = strings.TrimSpace(lines[i])
//			}
//			fieldDoc = strings.Join(lines, " ")
//		}
//
//		info.Fields = append(info.Fields, &FieldInfo{
//			Name: fieldName,
//			Type: fieldType,
//			Tag:  fieldTag,
//			Doc:  fieldDoc,
//			Typ:  typ,
//		})
//
//		baseType := fieldType
//
//		baseType = strings.TrimPrefix(baseType, "*")
//
//		baseType = strings.TrimPrefix(baseType, "[]")
//		baseType = strings.Split(baseType, ".")[0]
//		if skipTypes[baseType] {
//			continue
//		}
//		if !visited[baseType] {
//			visited[baseType] = true
//			collectFromImports(baseType)
//		}
//	}
//
//	structMap[name] = &info
//}
//
//func collectFromImports(typeName string) {
//	// future: support nested imports with doc.New(...)
//}
//
//func writeOutput(path string) {
//	var buf bytes.Buffer
//
//	buf.WriteString("# Storage Market Interface\n\n")
//	buf.WriteString("This document describes the storage market types and supported HTTP methods for making deals with Curio Storage Provider.\n\n")
//
//	buf.WriteString("## \U0001F4E1 MK20 HTTP API Overview\n\n")
//	buf.WriteString("The MK20 storage market module provides a set of HTTP endpoints under `/market/mk20` that allow clients to submit, track, and finalize storage deals with storage providers. This section documents all available routes and their expected behavior.\n\n")
//
//	buf.WriteString("### Base URL\n\n" +
//		"The base URL for all MK20 endpoints is: \n\n" +
//		"```\n\n/market/mk20\n\n```" +
//		"\n\n")
//
//	buf.WriteString("### 🔄 POST /store\n\n")
//	buf.WriteString("Submit a new MK20 deal.\n\n")
//	buf.WriteString("- **Content-Type**: N/A\n")
//	buf.WriteString("- **Body**: N/A\n")
//	buf.WriteString("- **Query Parameters**: N/A\n")
//	buf.WriteString("- **Response**:\n")
//	buf.WriteString("  - `200 OK`: Deal accepted\n")
//	buf.WriteString("  - Other [HTTP codes](#constants-for-errorcode) indicate validation failure, rejection, or system errors\n\n")
//
//	buf.WriteString("### 🧾 GET /status?id=<ulid>\n\n")
//	buf.WriteString("Retrieve the current status of a deal.\n\n")
//	buf.WriteString("- **Content-Type**: `application/json`\n")
//	buf.WriteString("- **Body**: N/A\n")
//	buf.WriteString("- **Query Parameters**:\n")
//	buf.WriteString("  - `id`: Deal identifier in [ULID](https://github.com/ulid/spec) format\n")
//	buf.WriteString("- **Response**:\n")
//	buf.WriteString("  - `200 OK`: JSON-encoded [deal status](#dealstatusresponse) information\n")
//	buf.WriteString("  - `400 Bad Request`: Missing or invalid ID\n")
//	buf.WriteString("  - `500 Internal Server Error`: If backend fails to respond\n\n")
//
//	buf.WriteString("### 📜 GET /contracts\n\n")
//	buf.WriteString("- **Content-Type**: N/A\n")
//	buf.WriteString("- **Body**: N/A\n")
//	buf.WriteString("- **Query Parameters**: N/A\n")
//	buf.WriteString("Return the list of contract addresses supported by the provider.\n\n")
//	buf.WriteString("- **Response**:\n")
//	buf.WriteString("  - `200 OK`: [JSON array of contract addresses](#supportedcontracts)\n")
//	buf.WriteString("  - `500 Internal Server Error`: Query or serialization failure\n\n")
//
//	buf.WriteString("### 🗂 PUT /data?id=<ulid>\n\n")
//	buf.WriteString("Upload deal data after the deal has been accepted.\n\n")
//	buf.WriteString("- **Content-Type**: `application/octet-stream`\n")
//	buf.WriteString("- **Body**: Deal data bytes\n")
//	buf.WriteString("- **Query Parameter**:\n -`id`: Deal identifier in [ULID](https://github.com/ulid/spec) format\n")
//	buf.WriteString("- **Headers**:\n")
//	buf.WriteString("  - `Content-Length`: must be deal's raw size\n")
//	buf.WriteString("- **Response**:\n")
//	buf.WriteString("  - `200 OK`: if data is successfully streamed\n")
//	buf.WriteString("  - `400`, `413`, or `415`: on validation failures\n\n")
//
//	buf.WriteString("### 🧠 GET /info\n\n")
//	buf.WriteString("- **Content-Type**: N/A\n")
//	buf.WriteString("- **Body**: N/A\n")
//	buf.WriteString("- **Query Parameters**: N/A\n")
//	buf.WriteString("Fetch markdown-formatted documentation that describes the supported deal schema, products, and data sources.\n\n")
//	buf.WriteString("- **Response**:\n")
//	buf.WriteString("  - `200 OK`: with markdown content of the info file\n")
//	buf.WriteString("  - `500 Internal Server Error`: if file is not found or cannot be read\n\n")
//
//	buf.WriteString("### 🧰 GET /products\n\n")
//	buf.WriteString("- **Content-Type**: N/A\n")
//	buf.WriteString("- **Body**: N/A\n")
//	buf.WriteString("- **Query Parameters**: N/A\n")
//	buf.WriteString("Fetch json list of the supported products.\n\n")
//	buf.WriteString("- **Response**:\n")
//	buf.WriteString("  - `200 OK`: with json content\n")
//	buf.WriteString("  - `500 Internal Server Error`: if info cannot be read\n\n")
//
//	buf.WriteString("### 🌐 GET /sources\n\n")
//	buf.WriteString("- **Content-Type**: N/A\n")
//	buf.WriteString("- **Body**: N/A\n")
//	buf.WriteString("- **Query Parameters**: N/A\n")
//	buf.WriteString("Fetch json list of the supported data sources.\n\n")
//	buf.WriteString("- **Response**:\n")
//	buf.WriteString("  - `200 OK`: with json content\n")
//	buf.WriteString("  - `500 Internal Server Error`: if info cannot be read\n\n")
//
//	buf.WriteString("## Supported Deal Types\n\n")
//	buf.WriteString("This document lists the data types and fields supported in the new storage market interface. It defines the deal structure, accepted data sources, and optional product extensions. Clients should use these definitions to format and validate their deals before submission.\n\n")
//
//	ordered := []string{"Deal", "DataSource", "Products"}
//	var rest []string
//	for k := range structMap {
//		if k != "Deal" && k != "DataSource" && k != "Products" {
//			rest = append(rest, k)
//		}
//	}
//	sort.Strings(rest)
//	keys := append(ordered, rest...)
//
//	for _, k := range keys {
//		s, ok := structMap[k]
//		if !ok {
//			continue
//		}
//		buf.WriteString(fmt.Sprintf("### %s\n\n", s.Name))
//		if s.Doc != "" {
//			buf.WriteString(s.Doc + "\n\n")
//		}
//		buf.WriteString("| Field | Type | Tag | Description |\n")
//		buf.WriteString("|-------|------|-----|-------------|\n")
//		for _, f := range s.Fields {
//			typeName := f.Type
//			linkTarget := ""
//
//			// Strip common wrappers like pointer/star and slice
//			trimmed := strings.TrimPrefix(typeName, "*")
//			trimmed = strings.TrimPrefix(trimmed, "[]")
//			parts := strings.Split(trimmed, ".")
//			baseType := parts[len(parts)-1]
//
//			if _, ok := structMap[baseType]; ok {
//				linkTarget = fmt.Sprintf("[%s](#%s)", f.Type, strings.ToLower(baseType))
//			} else if _, ok := constMap[baseType]; ok {
//				linkTarget = fmt.Sprintf("[%s](#constants-for-%s)", f.Type, strings.ToLower(baseType))
//			} else {
//				typ := f.Typ
//				if ptr, ok := typ.(*types.Pointer); ok {
//					typ = ptr.Elem()
//				}
//				if named, ok := typ.(*types.Named); ok && named.Obj() != nil && named.Obj().Pkg() != nil {
//
//					pkgPath := named.Obj().Pkg().Path()
//					objName := named.Obj().Name()
//					linkTarget = fmt.Sprintf("[%s](https://pkg.go.dev/%s#%s)", typeName, pkgPath, objName)
//				} else if typ != nil && typ.String() == baseType {
//					linkTarget = fmt.Sprintf("[%s](https://pkg.go.dev/builtin#%s)", typeName, baseType)
//				} else if slice, ok := typ.(*types.Slice); ok {
//					elem := slice.Elem()
//					if basic, ok := elem.(*types.Basic); ok {
//						linkTarget = fmt.Sprintf("[%s](https://pkg.go.dev/builtin#%s)", typeName, basic.Name())
//					} else {
//						linkTarget = typeName
//					}
//				} else {
//					linkTarget = typeName
//				}
//			}
//
//			buf.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n",
//				f.Name, linkTarget, strings.Trim(f.Tag, "`"), f.Doc))
//		}
//		buf.WriteString("\n")
//	}
//
//	// Render constants with sort order
//	for k, v := range constMap {
//		if len(v) == 0 {
//			continue
//		}
//		buf.WriteString(fmt.Sprintf("### Constants for %s\n\n", k))
//		buf.WriteString("| Constant | Code | Description |\n")
//		buf.WriteString("|----------|------|-------------|\n")
//		for _, c := range v {
//			buf.WriteString(fmt.Sprintf("| %s | %s | %s |\n", c.Name, c.Value, c.Doc))
//		}
//		buf.WriteString("\n")
//	}
//
//	os.Stdout.WriteString(buf.String())
//
//	err := os.WriteFile(path, buf.Bytes(), 0644)
//	if err != nil {
//		log.Fatalf("Failed to write output: %v", err)
//	}
//}
//
////func parseMux(path string) {
////	fset := token.NewFileSet()
////
////	node, err := parser.ParseFile(fset, path, nil, 0)
////	if err != nil {
////		log.Fatalf("Parse error: %v", err)
////	}
////
////	ast.Inspect(node, func(n ast.Node) bool {
////		call, ok := n.(*ast.CallExpr)
////		if !ok || len(call.Args) < 2 {
////			return true
////		}
////
////		sel, ok := call.Fun.(*ast.SelectorExpr)
////		if !ok {
////			return true
////		}
////
////		method := sel.Sel.Name
////		var path string
////		var fnName string
////
////		switch method {
////		case "Get", "Post", "Put", "Delete":
////			if len(call.Args) != 2 {
////				return true
////			}
////			pathLit, ok := call.Args[0].(*ast.BasicLit)
////			if !ok || pathLit.Kind != token.STRING {
////				return true
////			}
////			method = strings.ToUpper(method)
////			path = strings.Trim(pathLit.Value, "\"")
////			fnName = call.Args[1].(*ast.SelectorExpr).Sel.Name
////
////		case "Method":
////			if len(call.Args) != 3 {
////				return true
////			}
////			methodLit, ok := call.Args[0].(*ast.BasicLit)
////			if !ok || methodLit.Kind != token.STRING {
////				return true
////			}
////			method = strings.Trim(methodLit.Value, "\"")
////
////			pathLit, ok := call.Args[1].(*ast.BasicLit)
////			if !ok || pathLit.Kind != token.STRING {
////				return true
////			}
////			path = strings.Trim(pathLit.Value, "\"")
////			fnName = extractHandlerFunc(call.Args[2])
////
////		default:
////			return true
////		}
////
////		allHandlers[fnName] = &handlerInfo{
////			Path:     path,
////			Method:   method,
////			FuncName: fnName,
////			Errors:   map[string]bool{},
////		}
////		return true
////	})
////}
//
////func extractHandlerFunc(expr ast.Expr) string {
////	call, ok := expr.(*ast.CallExpr)
////	if !ok {
////		return "unknown"
////	}
////
////	switch fun := call.Fun.(type) {
////	case *ast.SelectorExpr:
////		if fun.Sel.Name == "TimeoutHandler" && len(call.Args) > 0 {
////			return extractHandlerFunc(call.Args[0])
////		}
////		if fun.Sel.Name == "HandlerFunc" && len(call.Args) > 0 {
////			if sel, ok := call.Args[0].(*ast.SelectorExpr); ok {
////				return sel.Sel.Name
////			}
////		}
////	}
////	return "unknown"
////}
//
////func parseHandlerBodies(path string) {
////	fset := token.NewFileSet()
////	file, err := parser.ParseFile(fset, path, nil, parser.AllErrors|parser.ParseComments)
////	if err != nil {
////		log.Fatalf("Parse error: %v", err)
////	}
////	for _, decl := range file.Decls {
////		fn, ok := decl.(*ast.FuncDecl)
////		if !ok || fn.Body == nil {
////			continue
////		}
////		name := fn.Name.Name
////		handler, exists := allHandlers[name]
////		if !exists {
////			continue
////		}
////
////		calls := map[string]bool{}
////		types := map[string]bool{}
////		constants := map[string]bool{}
////		var reqType string
////		var respType string
////
////		ast.Inspect(fn.Body, func(n ast.Node) bool {
////			switch node := n.(type) {
////			case *ast.CallExpr:
////				if sel, ok := node.Fun.(*ast.SelectorExpr); ok {
////					// http.WriteHeader or http.Error
////					if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "http" {
////						if sel.Sel.Name == "WriteHeader" || sel.Sel.Name == "Error" {
////							calls["http."+sel.Sel.Name] = true
////						}
////					}
////					// mdh.dm.MK20Handler.<Function>
////					if x1, ok := sel.X.(*ast.SelectorExpr); ok {
////						if x2, ok := x1.X.(*ast.SelectorExpr); ok {
////							if x2.X.(*ast.Ident).Name == "mdh" &&
////								x2.Sel.Name == "dm" &&
////								x1.Sel.Name == "MK20Handler" {
////								calls["mk20."+sel.Sel.Name] = true
////							}
////						}
////					}
////					// Detect json.Unmarshal(b, &type)
////					if sel.Sel.Name == "Unmarshal" && len(node.Args) == 2 {
////						if unary, ok := node.Args[1].(*ast.UnaryExpr); ok {
////							if ident, ok := unary.X.(*ast.Ident); ok {
////								reqType = findVarType(fn, ident.Name)
////							}
////						}
////					}
////					// Detect json.Marshal(type)
////					if sel.Sel.Name == "Marshal" && len(node.Args) == 1 {
////						if ident, ok := node.Args[0].(*ast.Ident); ok {
////							respType = findVarType(fn, ident.Name)
////						}
////					}
////				}
////			case *ast.AssignStmt:
////				for _, rhs := range node.Rhs {
////					if ce, ok := rhs.(*ast.CallExpr); ok {
////						if sel, ok := ce.Fun.(*ast.SelectorExpr); ok {
////							if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "http" {
////								if strings.HasPrefix(sel.Sel.Name, "Status") {
////									constants["http."+sel.Sel.Name] = true
////								}
////							}
////						}
////					}
////				}
////			case *ast.ValueSpec:
////				if node.Type != nil {
////					if se, ok := node.Type.(*ast.SelectorExpr); ok {
////						if ident, ok := se.X.(*ast.Ident); ok && ident.Name == "mk20" {
////							types["mk20."+se.Sel.Name] = true
////						}
////					}
////				}
////			case *ast.SelectorExpr:
////				if ident, ok := node.X.(*ast.Ident); ok && ident.Name == "http" {
////					if strings.HasPrefix(node.Sel.Name, "Status") {
////						constants["http."+node.Sel.Name] = true
////					}
////				}
////			}
////			return true
////		})
////
////		handler.Calls = calls
////		handler.Types = types
////		handler.Constants = constants
////		handler.RequestBodyType = reqType
////		handler.ResponseBodyType = respType
////	}
////}
////
////// Helper to find type of variable declared in function scope
////func findVarType(fn *ast.FuncDecl, name string) string {
////	for _, stmt := range fn.Body.List {
////		if ds, ok := stmt.(*ast.DeclStmt); ok {
////			if gd, ok := ds.Decl.(*ast.GenDecl); ok {
////				for _, spec := range gd.Specs {
////					if vs, ok := spec.(*ast.ValueSpec); ok {
////						for _, ident := range vs.Names {
////							if ident.Name == name {
////								if se, ok := vs.Type.(*ast.SelectorExpr); ok {
////									if x, ok := se.X.(*ast.Ident); ok {
////										return x.Name + "." + se.Sel.Name
////									}
////								}
////							}
////						}
////					}
////				}
////			}
////		}
////	}
////	return ""
////}
