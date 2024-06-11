package main

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	docgen_openrpc "github.com/filecoin-project/curio/api/docgen-openrpc"

	"github.com/filecoin-project/curio/api"
)

/*
main defines a small program that writes an OpenRPC document describing
a Curio API to stdout.
*/

func main() {
	Comments, GroupDocs := ParseApiASTInfo(os.Args[1], os.Args[2], os.Args[3], os.Args[4])

	doc := docgen_openrpc.NewCurioOpenRPCDocument(Comments, GroupDocs)

	i, _, _ := GetAPIType(os.Args[2], os.Args[3])
	doc.RegisterReceiverName("Filecoin", i)

	out, err := doc.Discover()
	if err != nil {
		log.Fatalln(err)
	}

	var jsonOut []byte
	var writer io.WriteCloser

	// Use os.Args to handle a somewhat hacky flag for the gzip option.
	// Could use flags package to handle this more cleanly, but that requires changes elsewhere
	// the scope of which just isn't warranted by this one use case which will usually be run
	// programmatically anyways.
	if len(os.Args) > 5 && os.Args[5] == "-gzip" {
		jsonOut, err = json.Marshal(out)
		if err != nil {
			log.Fatalln(err)
		}
		writer = gzip.NewWriter(os.Stdout)
	} else {
		jsonOut, err = json.MarshalIndent(out, "", "    ")
		if err != nil {
			log.Fatalln(err)
		}
		writer = os.Stdout
	}

	_, err = writer.Write(jsonOut)
	if err != nil {
		log.Fatalln(err)
	}
	err = writer.Close()
	if err != nil {
		log.Fatalln(err)
	}
}

type Visitor struct {
	Root    string
	Methods map[string]ast.Node
}

const NoComment = "There are not yet any comments for this method."

func ParseApiASTInfo(apiFile, iface, pkg, dir string) (comments map[string]string, groupDocs map[string]string) { //nolint:golint
	fset := token.NewFileSet()
	apiDir, err := filepath.Abs(dir)
	if err != nil {
		fmt.Println("./api filepath absolute error: ", err)
		return
	}
	apiFile, err = filepath.Abs(apiFile)
	if err != nil {
		fmt.Println("filepath absolute error: ", err, "file:", apiFile)
		return
	}
	pkgs, err := parser.ParseDir(fset, apiDir, nil, parser.AllErrors|parser.ParseComments)
	if err != nil {
		fmt.Println("parse error: ", err)
		return
	}

	ap := pkgs[pkg]

	f := ap.Files[apiFile]

	cmap := ast.NewCommentMap(fset, f, f.Comments)

	v := &Visitor{iface, make(map[string]ast.Node)}
	ast.Walk(v, ap)

	comments = make(map[string]string)
	groupDocs = make(map[string]string)
	for mn, node := range v.Methods {
		filteredComments := cmap.Filter(node).Comments()
		if len(filteredComments) == 0 {
			comments[mn] = NoComment
		} else {
			for _, c := range filteredComments {
				if strings.HasPrefix(c.Text(), "MethodGroup:") {
					parts := strings.Split(c.Text(), "\n")
					groupName := strings.TrimSpace(parts[0][12:])
					comment := strings.Join(parts[1:], "\n")
					groupDocs[groupName] = comment

					break
				}
			}

			l := len(filteredComments) - 1
			if len(filteredComments) > 1 {
				l = len(filteredComments) - 2
			}
			last := filteredComments[l].Text()
			if !strings.HasPrefix(last, "MethodGroup:") {
				comments[mn] = last
			} else {
				comments[mn] = NoComment
			}
		}
	}
	return comments, groupDocs
}

func (v *Visitor) Visit(node ast.Node) ast.Visitor {
	st, ok := node.(*ast.TypeSpec)
	if !ok {
		return v
	}

	if st.Name.Name != v.Root {
		return nil
	}

	iface := st.Type.(*ast.InterfaceType)
	for _, m := range iface.Methods.List {
		if len(m.Names) > 0 {
			v.Methods[m.Names[0].Name] = m
		}
	}

	return v
}

func GetAPIType(name, pkg string) (i interface{}, t reflect.Type, permStruct []reflect.Type) {
	switch pkg {
	case "api": // latest
		switch name {
		case "Curio":
			i = &api.CurioStruct{}
			t = reflect.TypeOf(new(struct{ api.Curio })).Elem()
			permStruct = append(permStruct, reflect.TypeOf(api.CurioStruct{}.Internal))
		default:
			panic("unknown type")
		}
	}
	return
}
