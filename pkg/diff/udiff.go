package diff

import (
	"errors"
	"fmt"
	"strings"

	"github.com/fatih/color"
	"github.com/sergi/go-diff/diffmatchpatch"
	"github.com/sourcegraph/go-diff/diff"
)

// UnifiedDiff represents a parsed unified diff
type UnifiedDiff struct {
	FileDiff *diff.FileDiff
}

// DiffLine represents a line in the unified diff with metadata
type DiffLine struct {
	Content string
	Type    DiffLineType
	Changes []DiffChange // Character-level changes within the line
}

// DiffLineType indicates whether a line is context, addition, or removal
type DiffLineType string

const (
	DiffLineContext DiffLineType = "context"
	DiffLineAdded   DiffLineType = "added"
	DiffLineRemoved DiffLineType = "removed"
)

// DiffChange represents a character-level change in a line
type DiffChange struct {
	Text string
	Type DiffChangeType
}

// DiffChangeType indicates whether a change is an addition, removal, or unchanged text
type DiffChangeType string

const (
	DiffChangeUnchanged DiffChangeType = "unchanged"
	DiffChangeAdded     DiffChangeType = "added"
	DiffChangeRemoved   DiffChangeType = "removed"
)

// DiffHunk represents a hunk in a unified diff
type DiffHunk struct {
	Header string
	Lines  []DiffLine
}

// ProcessedDiff represents a fully processed diff with all metadata
type ProcessedDiff struct {
	OrigFile string
	NewFile  string
	Hunks    []DiffHunk
}

// ParseUnifiedDiff parses a unified diff string into a structured format
func ParseUnifiedDiff(diffStr string) (*UnifiedDiff, error) {
	if diffStr == "" {
		return nil, errors.New("empty diff string")
	}

	lines := strings.Split(diffStr, "\n")
	commonWhitespace := ""
	for _, line := range lines[0] {
		if line == ' ' || line == '\t' {
			commonWhitespace += string(line)
		} else {
			break
		}
	}
	for i, line := range lines {
		lines[i] = strings.TrimPrefix(line, commonWhitespace)
	}
	diffStr = strings.Join(lines, "\n")

	// Parse the unified diff using sourcegraph/go-diff
	fileDiff, err := diff.ParseFileDiff([]byte(diffStr))
	if err != nil {
		// If parsing fails, fall back to our original implementation
		return nil, err
	}

	return &UnifiedDiff{
		FileDiff: fileDiff,
	}, nil
}

// ProcessDiff converts a UnifiedDiff into a ProcessedDiff
// This is the core logic that can be tested separately from color formatting
func (ud *UnifiedDiff) ProcessDiff() (*ProcessedDiff, error) {
	if ud == nil || ud.FileDiff == nil {
		return nil, errors.New("no diff data available")
	}

	result := &ProcessedDiff{
		OrigFile: ud.FileDiff.OrigName,
		NewFile:  ud.FileDiff.NewName,
	}

	// Process each hunk
	for _, hunk := range ud.FileDiff.Hunks {
		processedHunk := DiffHunk{
			Header: fmt.Sprintf("@@ -%d,%d +%d,%d @@%s",
				hunk.OrigStartLine, hunk.OrigLines,
				hunk.NewStartLine, hunk.NewLines,
				hunk.Section),
		}

		// Group related changes for better highlighting
		lineGroups := groupRelatedChanges(strings.Split(string(hunk.Body), "\n"))

		// Process each group of related changes
		for _, group := range lineGroups {
			// Add context lines
			for _, line := range group.contextLines {
				processedHunk.Lines = append(processedHunk.Lines, DiffLine{
					Content: line,
					Type:    DiffLineContext,
					Changes: []DiffChange{
						{Text: line, Type: DiffChangeUnchanged},
					},
				})
			}

			// Process changes - if we have a 1:1 mapping, do character-level diffing
			if len(group.oldLines) == 1 && len(group.newLines) == 1 {
				oldLine := group.oldLines[0]
				newLine := group.newLines[0]

				// Process the removed line with character-level changes
				removedLine := processLineChanges(oldLine, newLine, DiffLineRemoved)
				processedHunk.Lines = append(processedHunk.Lines, removedLine)

				// Process the added line with character-level changes
				addedLine := processLineChanges(oldLine, newLine, DiffLineAdded)
				processedHunk.Lines = append(processedHunk.Lines, addedLine)
			} else {
				// Add all removals
				for _, line := range group.oldLines {
					processedHunk.Lines = append(processedHunk.Lines, DiffLine{
						Content: line,
						Type:    DiffLineRemoved,
						Changes: []DiffChange{
							{Text: line, Type: DiffChangeRemoved},
						},
					})
				}

				// Add all additions
				for _, line := range group.newLines {
					processedHunk.Lines = append(processedHunk.Lines, DiffLine{
						Content: line,
						Type:    DiffLineAdded,
						Changes: []DiffChange{
							{Text: line, Type: DiffChangeAdded},
						},
					})
				}

				// Add all context lines
				for _, line := range group.contextLines {
					processedHunk.Lines = append(processedHunk.Lines, DiffLine{
						Content: line,
						Type:    DiffLineContext,
						Changes: []DiffChange{
							{Text: line, Type: DiffChangeUnchanged},
						},
					})
				}
			}
		}

		result.Hunks = append(result.Hunks, processedHunk)
	}

	return result, nil
}

// processLineChanges performs character-level diffing between two lines

// PrettyPrint formats the unified diff with colors
func (ud *UnifiedDiff) PrettyPrint() string {
	processed, err := ud.ProcessDiff()
	if err != nil {
		return err.Error()
	}

	return FormatDiff(processed)
}

// FormatDiff applies color formatting to a ProcessedDiff
func FormatDiff(diff *ProcessedDiff) string {
	prevNoColor := color.NoColor
	defer func() {
		color.NoColor = prevNoColor
	}()
	color.NoColor = false

	// Define formatting constants
	expectedPrefix := fmt.Sprintf("[%s] %s", color.New(color.FgBlue, color.Bold).Sprint("want"), color.New(color.Faint).Sprint(" +"))
	actualPrefix := fmt.Sprintf("[%s] %s", color.New(color.Bold, color.FgRed).Sprint("got"), color.New(color.Faint).Sprint("  -"))

	var result []string

	// Add file headers with proper formatting
	if diff.OrigFile != "" {
		result = append(result, fmt.Sprintf("%s %s [%s]",
			color.New(color.Faint).Sprint("---"),
			color.New(color.FgBlue).Sprint(diff.OrigFile),
			color.New(color.FgBlue, color.Bold).Sprint("want")))
	}
	if diff.NewFile != "" {
		result = append(result, fmt.Sprintf("%s %s [%s]",
			color.New(color.Faint).Sprint("+++"),
			color.New(color.FgRed).Sprint(diff.NewFile),
			color.New(color.FgRed, color.Bold).Sprint("got")))
	}

	// Process each hunk
	for _, hunk := range diff.Hunks {
		// Add hunk header
		result = append(result, color.New(color.Faint).Sprint(hunk.Header))

		// Process each line
		for _, line := range hunk.Lines {
			switch line.Type {
			case DiffLineContext:
				result = append(result, strings.Repeat(" ", 9)+formatStartingWhitespace(line.Content, color.New(color.Faint)))
			case DiffLineRemoved:
				if len(line.Changes) > 0 {
					// Format with character-level highlighting
					formatted := formatLineChanges(line, color.New(color.FgRed))
					result = append(result, actualPrefix+formatStartingWhitespace(formatted, color.New(color.FgRed)))
				} else {
					// Simple line removal
					result = append(result, actualPrefix+formatStartingWhitespace(line.Content, color.New(color.FgRed)))
				}
			case DiffLineAdded:
				if len(line.Changes) > 0 {
					// Format with character-level highlighting
					formatted := formatLineChanges(line, color.New(color.FgBlue))
					result = append(result, expectedPrefix+formatStartingWhitespace(formatted, color.New(color.FgBlue)))
				} else {
					// Simple line addition
					result = append(result, expectedPrefix+formatStartingWhitespace(line.Content, color.New(color.FgBlue)))
				}
			}
		}

		result = append(result, "") // Add blank line between hunks
	}

	return "\n" + strings.Join(result, "\n")
}

// formatLineChanges applies color formatting to character-level changes
func formatLineChanges(line DiffLine, lineColor *color.Color) string {
	var result strings.Builder

	for _, change := range line.Changes {
		switch change.Type {
		case DiffChangeUnchanged:
			result.WriteString(lineColor.Add(color.Faint).Sprint(change.Text))
		case DiffChangeAdded:
			result.WriteString(color.New(color.FgBlue, color.Bold).Sprint(change.Text))
		case DiffChangeRemoved:
			result.WriteString(color.New(color.FgRed, color.Bold).Sprint(change.Text))
		}
	}

	return result.String()
}

// lineGroup represents a group of related changes in a diff
type lineGroup struct {
	contextLines []string
	oldLines     []string
	newLines     []string
}

// groupRelatedChanges processes diff lines and groups them into related changes
func groupRelatedChanges(lines []string) []lineGroup {
	var groups []lineGroup
	var currentGroup lineGroup

	// Helper to add the current group to groups and start a new one
	addGroup := func() {
		if len(currentGroup.contextLines) > 0 || len(currentGroup.oldLines) > 0 || len(currentGroup.newLines) > 0 {
			groups = append(groups, currentGroup)
			currentGroup = lineGroup{
				contextLines: []string{},
				oldLines:     []string{},
				newLines:     []string{},
			}
		}
	}

	inChange := false

	for _, line := range lines {
		if line == "" {
			continue
		}

		switch line[0] {
		case '-', '+':
			// We're processing a change
			inChange = true

			if line[0] == '-' {
				currentGroup.oldLines = append(currentGroup.oldLines, line[1:])
			} else {
				currentGroup.newLines = append(currentGroup.newLines, line[1:])
			}
		case ' ':
			// Context line
			line = line[1:]
			fallthrough
		default:
			// Context line
			if inChange {
				// If we were processing changes and hit a context line,
				// finish the current group and start a new one
				addGroup()
				inChange = false
			}

			// Add to the current group's context lines
			if line == "" {
				line = "\t  "
			}
			currentGroup.contextLines = append(currentGroup.contextLines, line)
		}
	}

	// Add the final group if it's not empty
	addGroup()

	return groups
}

func processLineChanges(oldLine, newLine string, lineType DiffLineType) DiffLine {
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(oldLine, newLine, false)
	diffs = dmp.DiffCleanupSemantic(diffs)

	if len(diffs) == 0 {
		return DiffLine{
			Type:    lineType,
			Content: oldLine,
			Changes: []DiffChange{
				{Text: oldLine, Type: DiffChangeUnchanged},
			},
		}
	}

	result := DiffLine{
		Type:    lineType,
		Content: oldLine,
		Changes: []DiffChange{},
	}

	if lineType == DiffLineAdded {
		result.Content = newLine
	}

	// Process character-level changes based on line type
	for _, d := range diffs {
		changeType := DiffChangeUnchanged

		switch d.Type {
		case diffmatchpatch.DiffEqual:
			changeType = DiffChangeUnchanged
		case diffmatchpatch.DiffInsert:
			// Insertions only apply to added lines
			if lineType == DiffLineAdded {
				changeType = DiffChangeAdded
			} else if lineType == DiffLineRemoved {
				continue // Skip this change for removed lines
			}
		case diffmatchpatch.DiffDelete:
			// Deletions only apply to removed lines
			if lineType == DiffLineRemoved {
				changeType = DiffChangeRemoved
			} else if lineType == DiffLineAdded {
				continue // Skip this change for added lines
			}
		default:
			panic(fmt.Sprintf("unknown diff type: %d", d.Type))
		}

		result.Changes = append(result.Changes, DiffChange{
			Text: d.Text,
			Type: changeType,
		})
	}

	return result
}
