// enttui — codegen CLI that reads an ent schema directory and emits
// thin per-entity glue (one register_<name>.go per entity, plus a
// register_all.go aggregator) into a target package directory.
//
//	go run ./enttui/cmd/enttui \
//	    --schema /abs/path/to/dbent/schema \
//	    --out    /abs/path/to/enttui/examples/aicoder/gen \
//	    --package enttui \
//	    --ent-pkg dbent/gen/ent
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"enttui/codegen"
)

func main() {
	schema := flag.String("schema", "", "path to the ent schema directory")
	out := flag.String("out", "", "output directory for generated .go files")
	pkg := flag.String("package", "enttui", "package name declared in generated files")
	entPkg := flag.String("ent-pkg", "dbent/gen/ent", "import path of the generated ent client package")
	skipCSV := flag.String("skip", "", "comma-separated list of ent Type names to skip")
	flag.Parse()

	if *schema == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "usage: enttui --schema <dir> --out <dir> [--package enttui] [--ent-pkg dbent/gen/ent] [--skip A,B]")
		os.Exit(2)
	}

	skip := map[string]bool{}
	for _, s := range strings.Split(*skipCSV, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			skip[s] = true
		}
	}

	if err := codegen.Generate(codegen.Options{
		SchemaPath: *schema,
		OutDir:     *out,
		Package:    *pkg,
		EntPkgPath: *entPkg,
		Skip:       skip,
	}); err != nil {
		log.Fatalf("enttui: %v", err)
	}
	fmt.Fprintf(os.Stderr, "enttui: generated into %s\n", *out)
}
