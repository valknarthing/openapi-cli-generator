package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"go/format"
	"io/fs"
	"io/ioutil"
	"log"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/valknarthing/openapi-cli-generator/shorthand"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/spf13/cobra"
	yaml "gopkg.in/yaml.v2"
)

//go:embed templates/* cli/* shorthand/* apikey/* oauth/* auth0/*
var embeddedFS embed.FS

const baseModule = "github.com/valknarthing/openapi-cli-generator"

func getModuleName() string {
	data, err := ioutil.ReadFile("go.mod")
	if err != nil {
		return ""
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}

	return ""
}

func writeSupportFiles(moduleName string) {
	internalDir := "internal"
	if err := os.MkdirAll(internalDir, 0755); err != nil {
		panic(err)
	}

	folders := []string{"cli", "shorthand", "apikey", "oauth", "auth0"}

	for _, folder := range folders {
		err := fs.WalkDir(embeddedFS, folder, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if d.IsDir() {
				return os.MkdirAll(path.Join(internalDir, p), 0755)
			}

			if strings.HasSuffix(p, "_test.go") {
				return nil
			}

			data, err := embeddedFS.ReadFile(p)
			if err != nil {
				return err
			}

			content := string(data)
			// Rewrite imports
			content = strings.ReplaceAll(content, baseModule, moduleName+"/internal")

			// Write file
			return ioutil.WriteFile(path.Join(internalDir, p), []byte(content), 0644)
		})

		if err != nil {
			panic(err)
		}
	}
}

// OpenAPI Extensions
const (
	ExtAliases     = "x-cli-aliases"
	ExtDescription = "x-cli-description"
	ExtIgnore      = "x-cli-ignore"
	ExtHidden      = "x-cli-hidden"
	ExtName        = "x-cli-name"
	ExtWaiters     = "x-cli-waiters"
)

// Param describes an OpenAPI parameter (path, query, header, etc)
type Param struct {
	Name        string
	CLIName     string
	GoName      string
	Description string
	In          string
	Required    bool
	Type        string
	TypeNil     string
	Style       string
	Explode     bool
}

// Operation describes an OpenAPI operation (GET/POST/PUT/PATCH/DELETE)
type Operation struct {
	HandlerName    string
	GoName         string
	Use            string
	Aliases        []string
	Short          string
	Long           string
	Method         string
	CanHaveBody    bool
	ReturnType     string
	Path           string
	AllParams      []*Param
	RequiredParams []*Param
	OptionalParams []*Param
	MediaType      string
	Examples       []string
	Hidden         bool
	NeedsResponse  bool
	Tags           []string
	Waiters        []*WaiterParams
}

// Waiter describes a special command that blocks until a condition has been
// met, after which it exits.
type Waiter struct {
	CLIName     string
	GoName      string
	Use         string
	Aliases     []string
	Short       string
	Long        string
	Delay       int
	Attempts    int
	OperationID string `json:"operationId"`
	Operation   *Operation
	Matchers    []*Matcher
	After       map[string]map[string]string
}

// Matcher describes a condition to match for a waiter.
type Matcher struct {
	Select   string
	Test     string
	Expected json.RawMessage
	State    string
}

// WaiterParams links a waiter with param selector querires to perform wait
// operations after a command has run.
type WaiterParams struct {
	Waiter *Waiter
	Args   []string
	Params map[string]string
}

// Server describes an OpenAPI server endpoint
type Server struct {
	Description string
	URL         string
	// TODO: handle server parameters
}

// Imports describe optional imports based on features in use.
type Imports struct {
	Fmt     bool
	Strings bool
	Time    bool
}

// OpenAPI describes an API
type OpenAPI struct {
	Imports      Imports
	Name         string
	GoName       string
	PublicGoName string
	ModuleName   string
	Title        string
	Description  string
	Servers      []*Server
	Operations   []*Operation
	Waiters      []*Waiter
}

// ProcessAPI returns the API description to be used with the commands template
// for a loaded and dereferenced OpenAPI 3 document.
func ProcessAPI(shortName string, api *openapi3.T, moduleName string) *OpenAPI {
	apiName := shortName
	if api.Info.Extensions[ExtName] != nil {
		apiName = extStr(api.Info.Extensions[ExtName])
	}

	apiDescription := api.Info.Description
	if api.Info.Extensions[ExtDescription] != nil {
		apiDescription = extStr(api.Info.Extensions[ExtDescription])
	}

	result := &OpenAPI{
		Name:         apiName,
		GoName:       toGoName(shortName, false),
		PublicGoName: toGoName(shortName, true),
		ModuleName:   moduleName,
		Title:        api.Info.Title,
		Description:  apiDescription,
	}

	for _, s := range api.Servers {
		result.Servers = append(result.Servers, &Server{
			Description: s.Description,
			URL:         s.URL,
		})
	}

	// Convenience map for operation ID -> operation
	operationMap := make(map[string]*Operation)

	var keys []string
	for path := range api.Paths.Map() {
		keys = append(keys, path)
	}
	sort.Strings(keys)

	for _, path := range keys {
		pathItem := api.Paths.Find(path)

		if pathItem.Extensions[ExtIgnore] != nil {
			// Ignore this path.
			continue
		}

		pathHidden := false
		if pathItem.Extensions[ExtHidden] != nil {
			json.Unmarshal(pathItem.Extensions[ExtHidden].(json.RawMessage), &pathHidden)
		}

		for method, operation := range pathItem.Operations() {
			if operation.Extensions[ExtIgnore] != nil {
				// Ignore this operation.
				continue
			}

			name := operation.OperationID
			if operation.Extensions[ExtName] != nil {
				name = extStr(operation.Extensions[ExtName])
			}

			if name == "" {
				name = method + "-" + path
			}

			var aliases []string
			if operation.Extensions[ExtAliases] != nil {
				// We need to decode the raw extension value into our string slice.
				json.Unmarshal(operation.Extensions[ExtAliases].(json.RawMessage), &aliases)
			}

			params := getParams(pathItem, method)
			requiredParams := getRequiredParams(params)
			optionalParams := getOptionalParams(params)
			short := operation.Summary
			if short == "" {
				short = name
			}

			use := usage(name, requiredParams)

			description := operation.Description
			if operation.Extensions[ExtDescription] != nil {
				description = extStr(operation.Extensions[ExtDescription])
			}

			reqMt, reqSchema, reqExamples := getRequestInfo(operation)

			var examples []string
			if len(reqExamples) > 0 {
				wroteHeader := false
				for _, ex := range reqExamples {
					if _, ok := ex.(string); !ok {
						// Not a string, so it's structured data. Let's marshal it to the
						// shorthand syntax if we can.
						if m, ok := ex.(map[string]interface{}); ok {
							ex = shorthand.Get(m)
							examples = append(examples, ex.(string))
							continue
						}

						b, _ := json.Marshal(ex)

						if !wroteHeader {
							description += "\n## Input Example\n\n"
							wroteHeader = true
						}

						description += "\n" + string(b) + "\n"
						continue
					}

					if !wroteHeader {
						description += "\n## Input Example\n\n"
						wroteHeader = true
					}

					description += "\n" + ex.(string) + "\n"
				}
			}

			if reqSchema != "" {
				description += "\n## Request Schema (" + reqMt + ")\n\n" + reqSchema
			}

			methodTitle := strings.Title(strings.ToLower(method))

			hidden := pathHidden
			if operation.Extensions[ExtHidden] != nil {
				json.Unmarshal(operation.Extensions[ExtHidden].(json.RawMessage), &hidden)
			}

			returnType := "interface{}"
		returnTypeLoop:
			for code, ref := range operation.Responses.Map() {
				if num, err := strconv.Atoi(code); err != nil || num < 200 || num >= 300 {
					// Skip invalid responses
					continue
				}

				if ref.Value != nil {
					for _, content := range ref.Value.Content {
						if _, ok := content.Example.(map[string]interface{}); ok {
							returnType = "map[string]interface{}"
							break returnTypeLoop
						}

						if content.Schema != nil && content.Schema.Value != nil {
							if content.Schema.Value.Type.Is("object") || len(content.Schema.Value.Properties) != 0 {
								returnType = "map[string]interface{}"
								break returnTypeLoop
							}
						}
					}
				}
			}

			o := &Operation{
				HandlerName:    slug(name),
				GoName:         toGoName(name, true),
				Use:            use,
				Aliases:        aliases,
				Short:          short,
				Long:           description,
				Method:         methodTitle,
				CanHaveBody:    methodTitle == "Post" || methodTitle == "Put" || methodTitle == "Patch",
				ReturnType:     returnType,
				Path:           path,
				AllParams:      params,
				RequiredParams: requiredParams,
				OptionalParams: optionalParams,
				MediaType:      reqMt,
				Examples:       examples,
				Hidden:         hidden,
				Tags:           operation.Tags,
			}

			if operation.OperationID != "" {
				operationMap[operation.OperationID] = o
			}

			result.Operations = append(result.Operations, o)

			for _, p := range params {
				if p.In == "path" {
					result.Imports.Strings = true
				}
			}

			for _, p := range optionalParams {
				if p.In == "query" || p.In == "header" {
					result.Imports.Fmt = true
				}
			}
		}
	}

	if api.Extensions[ExtWaiters] != nil {
		var waiters map[string]*Waiter

		if err := json.Unmarshal(api.Extensions[ExtWaiters].(json.RawMessage), &waiters); err != nil {
			panic(err)
		}

		for name, waiter := range waiters {
			waiter.CLIName = slug(name)
			waiter.GoName = toGoName(name+"-waiter", true)
			waiter.Operation = operationMap[waiter.OperationID]
			waiter.Use = usage(name, waiter.Operation.RequiredParams)

			for _, matcher := range waiter.Matchers {
				if matcher.Test == "" {
					matcher.Test = "equal"
				}
			}

			for operationID, waitOpParams := range waiter.After {
				op := operationMap[operationID]
				if op == nil {
					panic(fmt.Errorf("Unknown waiter operation %s", operationID))
				}

				var args []string
				for _, p := range op.RequiredParams {
					selector := waitOpParams[p.Name]
					if selector == "" {
						panic(fmt.Errorf("Missing required parameter %s", p.Name))
					}
					delete(waitOpParams, p.Name)

					args = append(args, selector)

					result.Imports.Fmt = true
					op.NeedsResponse = true
				}

				// Transform from OpenAPI param names to CLI names
				wParams := make(map[string]string)
				for p, s := range waitOpParams {
					found := false
					for _, optional := range op.OptionalParams {
						if optional.Name == p {
							wParams[optional.CLIName] = s
							found = true
							break
						}
					}
					if !found {
						panic(fmt.Errorf("Unknown parameter %s for waiter %s", p, name))
					}
				}

				op.Waiters = append(op.Waiters, &WaiterParams{
					Waiter: waiter,
					Args:   args,
					Params: wParams,
				})
			}

			result.Waiters = append(result.Waiters, waiter)
		}

		if len(waiters) > 0 {
			result.Imports.Time = true
		}
	}

	return result
}

// extStr returns the string value of an OpenAPI extension stored as a JSON
// raw message or a string.
func extStr(i interface{}) string {
	if s, ok := i.(string); ok {
		return s
	}

	if b, ok := i.(json.RawMessage); ok {
		var decoded string
		if err := json.Unmarshal(b, &decoded); err != nil {
			panic(err)
		}
		return decoded
	}

	return fmt.Sprintf("%v", i)
}

func toGoName(input string, public bool) string {
	transformed := strings.Replace(input, "-", " ", -1)
	transformed = strings.Replace(transformed, "_", " ", -1)
	transformed = strings.Replace(transformed, "/", " ", -1)
	transformed = strings.Replace(transformed, "{", " ", -1)
	transformed = strings.Replace(transformed, "}", " ", -1)
	transformed = strings.Title(transformed)
	transformed = strings.Replace(transformed, " ", "", -1)

	if !public {
		transformed = strings.ToLower(string(transformed[0])) + transformed[1:]
	}

	return transformed
}

func slug(operationID string) string {
	transformed := strings.ToLower(operationID)
	transformed = strings.Replace(transformed, "_", "-", -1)
	transformed = strings.Replace(transformed, "/", "-", -1)
	transformed = strings.Replace(transformed, "{", "", -1)
	transformed = strings.Replace(transformed, "}", "", -1)
	transformed = strings.Replace(transformed, " ", "-", -1)
	return transformed
}

func usage(name string, requiredParams []*Param) string {
	usage := slug(name)

	for _, p := range requiredParams {
		usage += " " + slug(p.Name)
	}

	return usage
}

func getParams(path *openapi3.PathItem, httpMethod string) []*Param {
	operation := path.Operations()[httpMethod]
	allParams := make([]*Param, 0, len(path.Parameters))

	var total openapi3.Parameters
	total = append(total, path.Parameters...)
	total = append(total, operation.Parameters...)

	for _, p := range total {
		if p.Value != nil && p.Value.Extensions["x-cli-ignore"] == nil {
			t := "string"
			tn := "\"\""
			if p.Value.Schema != nil && p.Value.Schema.Value != nil && p.Value.Schema.Value.Type != nil {
				if p.Value.Schema.Value.Type.Is("boolean") {
					t = "bool"
					tn = "false"
				} else if p.Value.Schema.Value.Type.Is("integer") {
					t = "int64"
					tn = "0"
				} else if p.Value.Schema.Value.Type.Is("number") {
					t = "float64"
					tn = "0.0"
				}
			}

			cliName := slug(p.Value.Name)
			if p.Value.Extensions[ExtName] != nil {
				cliName = extStr(p.Value.Extensions[ExtName])
			}

			description := p.Value.Description
			if p.Value.Extensions[ExtDescription] != nil {
				description = extStr(p.Value.Extensions[ExtDescription])
			}

			allParams = append(allParams, &Param{
				Name:        p.Value.Name,
				CLIName:     cliName,
				GoName:      toGoName("param "+cliName, false),
				Description: description,
				In:          p.Value.In,
				Required:    p.Value.Required,
				Type:        t,
				TypeNil:     tn,
			})
		}
	}

	return allParams
}

func getRequiredParams(allParams []*Param) []*Param {
	required := make([]*Param, 0)

	for _, param := range allParams {
		if param.Required || param.In == "path" {
			required = append(required, param)
		}
	}

	return required
}

func getOptionalParams(allParams []*Param) []*Param {
	optional := make([]*Param, 0)

	for _, param := range allParams {
		if !param.Required && param.In != "path" {
			optional = append(optional, param)
		}
	}

	return optional
}

func getRequestInfo(op *openapi3.Operation) (string, string, []interface{}) {
	mts := make(map[string][]interface{})

	if op.RequestBody != nil && op.RequestBody.Value != nil {
		for mt, item := range op.RequestBody.Value.Content {
			var schema string
			var examples []interface{}

			if item.Schema != nil && item.Schema.Value != nil {
				// Let's make this a bit more concise. Since it has special JSON
				// marshalling functions, we do a dance to get it into plain JSON before
				// converting to YAML.
				data, err := json.Marshal(item.Schema.Value)
				if err != nil {
					continue
				}

				var unmarshalled interface{}
				json.Unmarshal(data, &unmarshalled)

				data, err = yaml.Marshal(unmarshalled)
				if err == nil {
					schema = string(data)
				}
			}

			if item.Example != nil {
				examples = append(examples, item.Example)
			} else {
				for _, ex := range item.Examples {
					if ex.Value != nil {
						examples = append(examples, ex.Value.Value)
						break
					}
				}
			}

			mts[mt] = []interface{}{schema, examples}
		}
	}

	// Prefer JSON.
	for mt, item := range mts {
		if strings.Contains(mt, "json") {
			return mt, item[0].(string), item[1].([]interface{})
		}
	}

	// Fall back to YAML next.
	for mt, item := range mts {
		if strings.Contains(mt, "yaml") {
			return mt, item[0].(string), item[1].([]interface{})
		}
	}

	// Last resort: return the first we find!
	for mt, item := range mts {
		return mt, item[0].(string), item[1].([]interface{})
	}

	return "", "", nil
}

func writeFormattedFile(filename string, data []byte) {
	formatted, errFormat := format.Source(data)
	if errFormat != nil {
		formatted = data
	}

	err := ioutil.WriteFile(filename, formatted, 0600)
	if errFormat != nil {
		panic(errFormat)
	} else if err != nil {
		panic(err)
	}
}

func initCmd(cmd *cobra.Command, args []string) {
	if _, err := os.Stat("main.go"); err == nil {
		fmt.Println("Refusing to overwrite existing main.go")
		return
	}

	moduleName := getModuleName()
	if moduleName == "" {
		fmt.Println("Warning: go.mod not found. Using app name as module path.")
		moduleName = args[0]
	}

	writeSupportFiles(moduleName)

	data, err := embeddedFS.ReadFile("templates/main.tmpl")
	if err != nil {
		panic(err)
	}
	tmpl, err := template.New("cli").Parse(string(data))
	if err != nil {
		panic(err)
	}

	templateData := map[string]string{
		"Name":       args[0],
		"NameEnv":    strings.Replace(strings.ToUpper(args[0]), "-", "_", -1),
		"ModuleName": moduleName,
	}

	var sb strings.Builder
	err = tmpl.Execute(&sb, templateData)
	if err != nil {
		panic(err)
	}

	writeFormattedFile("main.go", []byte(sb.String()))
}

func generate(cmd *cobra.Command, args []string) {
	data, err := ioutil.ReadFile(args[0])
	if err != nil {
		log.Fatal(err)
	}

	// Load the OpenAPI document.
	loader := openapi3.NewLoader()
	var swagger *openapi3.T
	swagger, err = loader.LoadFromData(data)
	if err != nil {
		log.Fatal(err)
	}

	moduleName := getModuleName()
	if moduleName == "" {
		moduleName = "main"
	}

	funcs := template.FuncMap{
		"quote": strconv.Quote,
		"slug":  slug,
		"title": strings.Title,
	}

	data, err = embeddedFS.ReadFile("templates/commands.tmpl")
	if err != nil {
		panic(err)
	}
	tmpl, err := template.New("cli").Funcs(funcs).Parse(string(data))
	if err != nil {
		panic(err)
	}

	shortName := strings.TrimSuffix(path.Base(args[0]), ".yaml")

	templateData := ProcessAPI(shortName, swagger, moduleName)

	var sb strings.Builder
	err = tmpl.Execute(&sb, templateData)
	if err != nil {
		panic(err)
	}

	writeFormattedFile(shortName+".go", []byte(sb.String()))

	// Generate README.md
	data, err = embeddedFS.ReadFile("templates/readme.tmpl")
	if err == nil {
		tmpl, err := template.New("readme").Funcs(funcs).Parse(string(data))
		if err == nil {
			var sbReadme strings.Builder
			err = tmpl.Execute(&sbReadme, templateData)
			if err == nil {
				ioutil.WriteFile("README.md", []byte(sbReadme.String()), 0644)
			}
		}
	}
}

func main() {
	root := &cobra.Command{}

	root.AddCommand(&cobra.Command{
		Use:   "init <app-name>",
		Short: "Initialize and generate a `main.go` file for your project",
		Args:  cobra.ExactArgs(1),
		Run:   initCmd,
	})

	root.AddCommand(&cobra.Command{
		Use:   "generate <api-spec>",
		Short: "Generate a `commands.go` file from an OpenAPI spec",
		Args:  cobra.ExactArgs(1),
		Run:   generate,
	})

	root.Execute()
}
