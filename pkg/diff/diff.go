package diff

import (
	"reflect"

	"github.com/fatih/color"
	"github.com/google/go-cmp/cmp"
	"github.com/k0kubun/pp/v3"

	"github.com/pmezard/go-difflib/difflib"
	"github.com/sergi/go-diff/diffmatchpatch"
)

func TypedDiffExportedOnly[T any](want T, got T) string {
	printer := pp.New()
	printer.SetExportedOnly(true)
	printer.SetColoringEnabled(false)

	return diffTyped(printer, want, got)
}

func TypedDiff[T any](want T, got T, opts ...OptTestingOptsSetter) string {
	printer := pp.New()
	printer.SetExportedOnly(false)
	printer.SetColoringEnabled(false)

	return diffTyped(printer, want, got, opts...)
}

func diffd(want string, got string) string {
	diff, _ := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(want),
		B:        difflib.SplitLines(got),
		FromFile: "Expected",
		FromDate: "",
		ToFile:   "Actual",
		ToDate:   "",
		Context:  5,
	})

	return diff

}

// formatStartingWhitespace formats leading whitespace characters to be visible while maintaining proper spacing
// Example:
//
//	Input:  "    \t  hello"
//	Output: "····→···hello"
//
// Where:
//
//	· represents a space (Middle Dot U+00B7)
//	→ represents a tab (Rightwards Arrow U+2192)
func formatStartingWhitespace(s string, colord *color.Color) string {
	out := color.New(color.Bold).Sprint(" | ")
	for j, char := range s {
		switch char {
		case ' ':
			out += color.New(color.Faint, color.FgHiGreen).Sprint("∙") // ⌷
		case '\t':
			out += color.New(color.Faint, color.FgHiGreen).Sprint("→   ") // → └──▹
		default:
			return out + colord.Sprint(s[j:])
		}
	}
	return out
}

func diffTyped[T any](printer *pp.PrettyPrinter, want T, got T, opts ...OptTestingOptsSetter) string {
	// Enable colors

	// printer.WithLineInfo = true

	switch any(want).(type) {
	case reflect.Type:
		want := ConvolutedFormatReflectType(any(want).(reflect.Type))
		got := ConvolutedFormatReflectType(any(got).(reflect.Type))
		return diffTyped(printer, want, got, opts...)
	case reflect.Value:
		w := any(want).(reflect.Value)
		g := any(got).(reflect.Value)
		want := ConvolutedFormatReflectValue(w)
		got := ConvolutedFormatReflectValue(g)
		return diffTyped(printer, want, got, opts...)
	case string:
		unified := diffd(any(want).(string), any(got).(string))
		ud, err := ParseUnifiedDiff(unified)
		if err != nil {
			opts := NewTestingOpts(opts...)

			return EnrichCmpDiff(cmp.Diff(got, want, opts.cmpOpts...))
		}

		// return EnrichUnifiedDiff(unified)
		return ud.PrettyPrint()
	default:
		opts := NewTestingOpts(opts...)
		cmpd := cmp.Diff(got, want, opts.cmpOpts...)
		return EnrichCmpDiff(cmpd)
	}
}

func SingleLineStringDiff(want string, got string) string {
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(want, got, false)
	return dmp.DiffPrettyText(diffs)
}
