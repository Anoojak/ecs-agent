//go:build codegen

// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//	http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

// Note: This package uses the 'codegen' directive for compatibility with
// the  AWS SDK's code generation scripts.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/aws/aws-sdk-go/private/model/api"
	"github.com/aws/aws-sdk-go/private/util"
	"golang.org/x/tools/imports"
)

func main() {
	os.Exit(_main())
}

func ShapeListWithErrors(a *api.API) []*api.Shape {
	list := make([]*api.Shape, 0, len(a.Shapes))
	for _, n := range a.ShapeNames() {
		list = append(list, a.Shapes[n])
	}
	return list
}

var typesOnlyTplAPI = template.Must(template.New("api").Funcs(template.FuncMap{
	"ShapeListWithErrors": ShapeListWithErrors,
}).Parse(`
{{ $shapeList := ShapeListWithErrors $ }}
{{ range $_, $s := $shapeList }}
{{ if eq $s.Type "structure"}}{{ $s.GoCode }}{{ end }}

{{ end }}
`))

var (
	typesOnly       bool
	copyrightHeader string
)

func init() {
	types := flag.Bool("typesOnly", false, "only generate types")
	copyrightFile := flag.String("copyright_file", "", "Copyright file used to add copyright header")
	flag.Parse()

	if *copyrightFile == "" {
		fmt.Println("copyright_file must be set")
		os.Exit(1)
	}

	copyrightText, err := getCopyrightFile(*copyrightFile)
	if err != nil {
		fmt.Println("error reading copyright_file: ", err)
		os.Exit(1)
	}

	copyrightText += "\nCode generated by [agent/gogenerate/awssdk.go] DO NOT EDIT."

	b := &strings.Builder{}
	for i, line := range strings.Split(copyrightText, "\n") {
		if i != 0 {
			b.WriteString("\n")
		}
		b.WriteString("//")
		if line != "" {
			b.WriteString(" ")
			b.WriteString(line)
		}
	}
	b.WriteString("\n")

	copyrightHeader = b.String()
	typesOnly = *types
}

// return-based exit code so the 'defer' works
func _main() int {
	apiFile := "./api/api-2.json"
	var err error
	if typesOnly {
		err = genTypesOnlyAPI(apiFile)
	} else {
		err = genFull(apiFile)
	}
	if err != nil {
		fmt.Println(err)
		return 1
	}
	return 0
}

func genTypesOnlyAPI(file string) error {
	apiGen := &api.API{
		NoRemoveUnusedShapes:      true,
		NoRenameToplevelShapes:    true,
		NoGenStructFieldAccessors: true,
	}
	apiGen.Attach(file)
	apiGen.Setup()
	// to reset imports so that timestamp has an entry in the map.
	apiGen.APIGoCode()

	var buf bytes.Buffer
	err := typesOnlyTplAPI.Execute(&buf, apiGen)
	if err != nil {
		panic(err)
	}
	code := strings.TrimSpace(buf.String())
	code = util.GoFmt(code)

	// Ignore dir error, filepath will catch it for an invalid path.
	os.Mkdir(apiGen.PackageName(), 0755)
	// Fix imports.
	codeWithImports, err := imports.Process("", []byte(fmt.Sprintf("package %s\n\n%s", apiGen.PackageName(), code)), nil)
	if err != nil {
		fmt.Println(err)
		return err
	}
	outFile := filepath.Join(apiGen.PackageName(), "api.go")
	err = ioutil.WriteFile(outFile, []byte(fmt.Sprintf("%s\n%s", copyrightHeader, codeWithImports)), 0644)
	if err != nil {
		return err
	}
	return nil
}

func genFull(file string) error {
	// Heavily inspired by https://github.com/aws/aws-sdk-go/blob/45ba2f817fbf625fe48cf9b51fae13e5dfba6171/internal/model/cli/gen-api/main.go#L36
	// That code is copyright Amazon
	api := &api.API{}
	api.Attach(file)
	paginatorsFile := strings.Replace(file, "api-2.json", "paginators-1.json", -1)
	if _, err := os.Stat(paginatorsFile); err == nil {
		api.AttachPaginators(paginatorsFile)
	}

	docsFile := strings.Replace(file, "api-2.json", "docs-2.json", -1)
	if _, err := os.Stat(docsFile); err == nil {
		api.AttachDocs(docsFile)
	}
	api.Setup()

	if err := genFile(api, api.APIGoCode(), "api.go"); err != nil {
		return err
	}

	if err := genFile(api, api.ServiceGoCode(), "service.go"); err != nil {
		return err
	}

	if err := genFile(api, api.APIErrorsGoCode(), "errors.go"); err != nil {
		return err
	}

	return nil
}

func genFile(api *api.API, code string, fileName string) error {
	processedCode, err := imports.Process("",
		[]byte(fmt.Sprintf("package %s\n\n%s", api.PackageName(), code)),
		nil)
	if err != nil {
		fmt.Println("Error processing file imports: ", err)
		return err
	}
	// Ignore dir error, filepath will catch it for an invalid path.
	os.Mkdir(api.PackageName(), 0755)
	outFile, err := os.Create(filepath.Join(api.PackageName(), fileName))
	if err != nil {
		fmt.Println("Error creating file: ", err)
		return err
	}
	defer outFile.Close()
	withHeader := fmt.Sprintf("%s\n%s", copyrightHeader, processedCode)
	goimports := exec.Command("goimports")
	goimports.Stdin = bytes.NewBufferString(withHeader)
	goimports.Stdout = outFile
	err = goimports.Run()
	if err != nil {
		fmt.Println("Error running goimports: ", err)
		return err
	}

	return nil
}

func getCopyrightFile(filename string) (string, error) {
	contents, err := ioutil.ReadFile(filename)
	return string(contents), err
}
