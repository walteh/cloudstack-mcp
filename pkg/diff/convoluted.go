package diff

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"sort"
	"strings"

	"github.com/walteh/yaml"
)

func ConvolutedFormatReflectValue(s reflect.Value) any {

	if !s.IsValid() {
		return s.String()
	}

	buf := bytes.NewBuffer(nil)
	enc := json.NewEncoder(buf)
	enc.SetIndent("", "\t")

	if err := enc.Encode(s.Interface()); err != nil {
		panic(err)
	}

	mapd := yaml.NewOrderedMap()

	if err := json.Unmarshal(buf.Bytes(), mapd); err != nil {
		panic(err)
	}
	ms := mapd.ToMapSlice()
	keys := []string{}
	for _, key := range ms {
		keys = append(keys, key.Key.(string))
	}
	sort.Strings(keys)

	ms.SortKeys(keys...)

	buf.Reset()

	enc2 := json.NewEncoder(buf)
	enc2.SetIndent("", "\t")
	if err := enc2.Encode(ms); err != nil {
		panic(err)
	}

	return buf.String()
}

// FormatStructString takes a string containing a Go struct definition and formats it for better readability
func ConvolutedFormatReflectType(s reflect.Type) string {
	if !strings.Contains(s.String(), "struct {") {
		return s.String()
	}

	// Create a valid Go file from the struct
	src := fmt.Sprintf("package p\ntype T %s", s)

	src = strings.ReplaceAll(src, "\\\"", "$$$$")
	src = strings.ReplaceAll(src, "\"", "`")
	src = strings.ReplaceAll(src, "$$$$", "\"")

	// Parse the file
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return s.String() // Return original if parsing fails
	}

	// Find the struct type
	var structType *ast.StructType
	ast.Inspect(file, func(n ast.Node) bool {
		if t, ok := n.(*ast.StructType); ok {
			structType = t
			return false
		}
		return true
	})

	if structType == nil {
		return s.String()
	}

	// Format the struct
	return formatStructAST(structType, 0)
}

// formatStructAST formats a struct AST node
func formatStructAST(structType *ast.StructType, depth int) string {
	if structType == nil || structType.Fields == nil {
		return ""
	}

	// Collect and sort fields
	var fields []string
	for _, field := range structType.Fields.List {
		fieldStr := formatField(field, depth+1)
		if fieldStr != "" {
			fields = append(fields, fieldStr)
		}
	}
	sort.Strings(fields)

	// Build the formatted struct
	var result strings.Builder
	result.WriteString("struct {\n")
	for i, field := range fields {
		result.WriteString(strings.Repeat("\t", depth+1))
		result.WriteString(field)
		if i < len(fields)-1 {
			result.WriteString("\n")
		}
	}
	if len(fields) > 0 {
		result.WriteString("\n")
	}
	result.WriteString(strings.Repeat("\t", depth) + "}")

	return result.String()
}

// formatField formats a single struct field
func formatField(field *ast.Field, depth int) string {
	if field == nil {
		return ""
	}

	var name string
	if len(field.Names) > 0 {
		name = field.Names[0].Name
	}

	// Format the type
	typeStr := formatType(field.Type, depth)

	// Format the tag if present
	var tagStr string
	if field.Tag != nil {
		tagStr = field.Tag.Value
	}

	// Build the field string
	var parts []string
	if name != "" {
		parts = append(parts, name)
	}
	parts = append(parts, typeStr)
	if tagStr != "" {
		parts = append(parts, tagStr)
	}

	return strings.Join(parts, " ")
}

// formatType formats a type AST node
func formatType(expr ast.Expr, depth int) string {
	if expr == nil {
		return ""
	}

	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + formatType(t.X, depth)
	case *ast.ArrayType:
		return "[]" + formatType(t.Elt, depth)
	case *ast.MapType:
		return fmt.Sprintf("map[%s]%s", formatType(t.Key, depth), formatType(t.Value, depth))
	case *ast.StructType:
		return formatStructAST(t, depth)
	default:
		return fmt.Sprintf("%#v", expr)
	}
}
