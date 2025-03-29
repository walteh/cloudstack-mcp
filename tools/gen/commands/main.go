package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func main() {
	// Define directories and patterns
	cmdDir := "../../cmd/vmctl/commands"
	rootFile := filepath.Join(cmdDir, "root.go")

	// Find all command files
	commandFiles, err := findCommandFiles(cmdDir)
	if err != nil {
		fmt.Printf("Error finding command files: %v\n", err)
		os.Exit(1)
	}

	// Extract command variables from files
	commands, err := extractCommands(commandFiles)
	if err != nil {
		fmt.Printf("Error extracting commands: %v\n", err)
		os.Exit(1)
	}

	// Update root.go file with commands
	err = updateRootFile(rootFile, commands)
	if err != nil {
		fmt.Printf("Error updating root file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully updated root.go with %d commands\n", len(commands))
}

// findCommandFiles looks for Go files in the command directory that contain command definitions
func findCommandFiles(dir string) ([]string, error) {
	var files []string

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if strings.HasSuffix(name, ".go") && name != "root.go" {
			files = append(files, filepath.Join(dir, name))
		}
	}

	return files, nil
}

// extractCommands parses Go files to find command variable declarations
func extractCommands(files []string) ([]string, error) {
	var commands []string
	fset := token.NewFileSet()

	for _, file := range files {
		node, err := parser.ParseFile(fset, file, nil, parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("error parsing %s: %v", file, err)
		}

		for _, decl := range node.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.VAR {
				continue
			}

			for _, spec := range genDecl.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}

				for _, name := range valueSpec.Names {
					if strings.HasSuffix(name.Name, "Cmd") {
						commands = append(commands, name.Name)
					}
				}
			}
		}
	}

	return commands, nil
}

// updateRootFile updates the root.go file with init function that adds all commands
func updateRootFile(rootFile string, commands []string) error {
	data, err := ioutil.ReadFile(rootFile)
	if err != nil {
		return err
	}

	content := string(data)

	// Find the init function
	initRegex := regexp.MustCompile(`func init\(\) {[\s\S]*?}`)
	initMatch := initRegex.FindString(content)

	if initMatch == "" {
		// If no init function exists, create one
		initFunc := `
func init() {
	// Define persistent flags that apply to all commands
	RootCmd.PersistentFlags().BoolVarP(&Debug, "debug", "d", false, "Enable debug logging")
	
	// The following line is used by go generate to add commands
	//go:generate go run ../../tools/gen/commands/main.go
`

		for _, cmd := range commands {
			initFunc += fmt.Sprintf("\tRootCmd.AddCommand(%s)\n", cmd)
		}

		initFunc += "}\n"

		// Add the init function before the last closing brace
		lastBraceIndex := strings.LastIndex(content, "}")
		if lastBraceIndex == -1 {
			return fmt.Errorf("could not find closing brace in root.go")
		}

		content = content[:lastBraceIndex] + initFunc + content[lastBraceIndex:]
	} else {
		// If init function exists, update it with commands
		commandsSection := ""
		for _, cmd := range commands {
			commandsSection += fmt.Sprintf("\tRootCmd.AddCommand(%s)\n", cmd)
		}

		// Find position to insert commands (after the go:generate comment)
		generateCommentRegex := regexp.MustCompile(`\s*//go:generate.*\n`)
		generateCommentMatch := generateCommentRegex.FindString(initMatch)

		if generateCommentMatch != "" {
			// Insert after the go:generate comment
			newInitFunc := strings.Replace(
				initMatch,
				generateCommentMatch,
				generateCommentMatch+commandsSection,
				1,
			)
			content = strings.Replace(content, initMatch, newInitFunc, 1)
		} else {
			// Insert at the end of the init function
			lastBraceIndex := strings.LastIndex(initMatch, "}")
			if lastBraceIndex == -1 {
				return fmt.Errorf("could not find closing brace in init function")
			}

			newInitFunc := initMatch[:lastBraceIndex] + commandsSection + initMatch[lastBraceIndex:]
			content = strings.Replace(content, initMatch, newInitFunc, 1)
		}
	}

	return ioutil.WriteFile(rootFile, []byte(content), 0644)
}
