// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"log"
	"os"
	"sort"
	"strings"

	"go/types"

	"golang.org/x/tools/go/packages"
)

var (
	reportExported = flag.Bool("e", false, "Report exported variables and constants")
	buildTags      = flag.String("tags", "", "Build tags")
)

type object struct {
	pkgPath string
	name    string
}

type visitor struct {
	pkg        *packages.Package
	uses       map[object]int
	positions  map[object]token.Position
	insideFunc bool
}

func getKey(obj types.Object) object {
	if obj == nil {
		return object{}
	}

	pkg := obj.Pkg()
	pkgPath := ""
	if pkg != nil {
		pkgPath = pkg.Path()
	}

	return object{
		pkgPath: pkgPath,
		name:    obj.Name(),
	}
}

func (v *visitor) decl(obj types.Object) {
	key := getKey(obj)
	if _, ok := v.uses[key]; !ok {
		v.uses[key] = 0
	}
	if _, ok := v.positions[key]; !ok {
		v.positions[key] = v.pkg.Fset.Position(obj.Pos())
	}
}

func (v *visitor) use(obj types.Object) {
	key := getKey(obj)
	if _, ok := v.uses[key]; ok {
		v.uses[key]++
	} else {
		v.uses[key] = 1
	}
}

func isReserved(name string) bool {
	return name == "_" || strings.HasPrefix(strings.ToLower(name), "_cgo_")
}

func (v *visitor) Visit(node ast.Node) ast.Visitor {
	switch node := node.(type) {
	case *ast.Ident:
		v.use(v.pkg.TypesInfo.Uses[node])

	case *ast.ValueSpec:
		if !v.insideFunc {
			for _, ident := range node.Names {
				if !isReserved(ident.Name) {
					v.decl(v.pkg.TypesInfo.Defs[ident])
				}
			}
		}
		for _, val := range node.Values {
			ast.Walk(v, val)
		}
		if node.Type != nil {
			ast.Walk(v, node.Type)
		}
		return nil

	case *ast.FuncDecl:
		if node.Body != nil {
			v.insideFunc = true
			ast.Walk(v, node.Body)
			v.insideFunc = false
		}

		if node.Recv != nil {
			ast.Walk(v, node.Recv)
		}
		if node.Type != nil {
			ast.Walk(v, node.Type)
		}

		return nil
	}

	return v
}

func main() {
	flag.Parse()
	exitStatus := 0
	importPaths := flag.Args()
	if len(importPaths) == 0 {
		importPaths = []string{"."}
	}

	var flags []string
	if *buildTags != "" {
		flags = append(flags, fmt.Sprintf("-tags=%s", *buildTags))
	}
	cfg := &packages.Config{
		Mode:       packages.LoadSyntax,
		Tests:      true,
		BuildFlags: flags,
	}
	pkgs, err := packages.Load(cfg, importPaths...)
	if err != nil {
		log.Fatalf("could not load packages: %s", err)
	}

	uses := make(map[object]int)
	positions := make(map[object]token.Position)

	for _, pkg := range pkgs {
		v := &visitor{
			pkg:       pkg,
			uses:      uses,
			positions: positions,
		}

		for _, f := range v.pkg.Syntax {
			ast.Walk(v, f)
		}
	}

	var lines []string

	for obj, useCount := range uses {
		if useCount == 0 && (*reportExported || !ast.IsExported(obj.name)) {
			pos := positions[obj]
			lines = append(lines, fmt.Sprintf("%s: %s:%d:%d: %s", obj.pkgPath, pos.Filename, pos.Line, pos.Column, obj.name))
			exitStatus = 1
		}
	}

	sort.Strings(lines)
	for _, line := range lines {
		fmt.Println(line)
	}

	os.Exit(exitStatus)
}
