package diff

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/stretchr/testify/require"
)

func unknownValueEqualAsJSON(t *testing.T, want, got reflect.Value) bool {
	td := TypedDiff(want, got)
	if td != "" {
		color.NoColor = false
		str := color.New(color.FgHiYellow, color.Faint).Sprintf("\n\n============= VALUE COMPARISON START =============\n\n")
		str += fmt.Sprintf("%s\n\n", color.YellowString("%s", t.Name()))
		str += fmt.Sprintf("want type: %s\n", color.YellowString(want.Type().String()))
		str += fmt.Sprintf("got type:  %s\n\n\n", color.YellowString(got.Type().String()))
		str += shortenOutputIfNeeded(td) + "\n\n"
		str += color.New(color.FgHiYellow, color.Faint).Sprintf("============= VALUE COMPARISON END ===============\n\n")
		t.Log("value comparison report:\n" + str)
		return false
	}
	return true
}

func unknownTypeEqual(t *testing.T, want, got reflect.Type) bool {
	t.Helper()
	td := TypedDiff(want, got)
	if td != "" {
		color.NoColor = false
		str := color.New(color.FgHiYellow, color.Faint).Sprintf("\n\n============= VALUE COMPARISON START =============\n\n")
		str += fmt.Sprintf("%s\n\n", color.YellowString("%s", t.Name()))
		str += fmt.Sprintf("want type: %s\n", color.YellowString(want.String()))
		str += fmt.Sprintf("got type:  %s\n\n\n", color.YellowString(got.String()))
		str += shortenOutputIfNeeded(td) + "\n\n"
		str += color.New(color.FgHiYellow, color.Faint).Sprintf("============= VALUE COMPARISON END ===============\n\n")
		t.Log("value comparison report:\n" + str)
		return false
	}
	return true
}

const ansi = "[\u001B\u009B][[\\]()#;?]*(?:(?:(?:[a-zA-Z\\d]*(?:;[a-zA-Z\\d]*)*)?\u0007)|(?:(?:\\d{1,4}(?:;\\d{0,4})*)?[\\dA-PRZcf-ntqry=><~]))"

var re = regexp.MustCompile(ansi)

func Strip(str string) string {
	return re.ReplaceAllString(str, "")
}

func shortenOutputIfNeeded(s string) string {
	lines := strings.Split(s, "\n")
	var result []string
	var buffer []string

	// Helper to process a batch of lines when we're ready to flush them
	flushBuffer := func(buf []string) {
		if len(buf) >= 10 {
			// Add first 5 lines
			result = append(result, buf[:5]...)
			// Add truncation message
			result = append(result, fmt.Sprintf("...truncated %d lines...", len(buf)-10))
			// Add last 5 lines
			result = append(result, buf[len(buf)-5:]...)
		} else {
			// If less than 10 lines, just add them all
			result = append(result, buf...)
		}
	}

	for _, line := range lines {
		stripped := Strip(line)
		isAct := strings.HasPrefix(stripped, "[act]")
		isExp := strings.HasPrefix(stripped, "[exp]")

		// If this is a continuation of the current buffer type
		if (isAct && len(buffer) > 0 && strings.HasPrefix(Strip(buffer[0]), "[act]")) ||
			(isExp && len(buffer) > 0 && strings.HasPrefix(Strip(buffer[0]), "[exp]")) {
			buffer = append(buffer, line)
			continue
		}

		// If we hit a different type of line, flush the buffer
		if len(buffer) > 0 {
			flushBuffer(buffer)
			buffer = nil
		}

		// Start a new buffer if this is an act/exp line
		if isAct || isExp {
			buffer = []string{line}
		} else {
			// Regular line, just add it
			result = append(result, line)
		}
	}

	// Don't forget to flush any remaining buffer
	if len(buffer) > 0 {
		flushBuffer(buffer)
	}

	return strings.Join(result, "\n")
}

func knownTypeEqual[T any](t *testing.T, want, got T) bool {
	t.Helper()
	td := TypedDiff(want, got)
	if td != "" {
		color.NoColor = false
		str := color.New(color.FgHiYellow, color.Faint).Sprintf("\n\n============= TYPE COMPARISON START =============\n\n")
		str += fmt.Sprintf("%s\n\n", color.YellowString("%s", t.Name()))
		str += fmt.Sprintf("type: %s\n", color.YellowString(reflect.TypeOf(want).String()))
		str += shortenOutputIfNeeded(td) + "\n\n"
		str += color.New(color.FgHiYellow, color.Faint).Sprintf("============= TYPE COMPARISON END ===============\n\n")
		t.Log("type comparison report:\n" + str)
		return false
	}
	return true
}

func RequireUnknownTypeEqual(t *testing.T, want, got reflect.Type) {
	t.Helper()
	if !unknownTypeEqual(t, want, got) {
		require.Fail(t, "type mismatch")
	}
}

func RequireUnknownValueEqualAsJSON(t *testing.T, want, got reflect.Value) {
	t.Helper()
	if !unknownValueEqualAsJSON(t, want, got) {
		require.Fail(t, "value mismatch")
	}
}

func RequireKnownValueEqual[T any](t *testing.T, want, got T) {
	t.Helper()
	if !knownTypeEqual(t, want, got) {
		require.Fail(t, "value mismatch")
	}
}
