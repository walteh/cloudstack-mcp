package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

var commandToRun []string
var commandsToPipe [][]string

func findNextSeparator(args []string, idx int) int {
	for i := idx; i < len(args); i++ {
		if args[i] == "---" {
			return i
		}
	}
	return -1
}

func isolateCommands(args []string) ([]string, [][]string) {
	// fmt.Printf("os.Args: %v\n", os.Args)

	// things are passed to this command as
	// this --- command to run --- command to pipe 1 --- command to pipe 2 --- ...

	idx := findNextSeparator(os.Args, 0)
	if idx == -1 {
		fmt.Println("no commands found")
		os.Exit(1)
	}

	commands := [][]string{}
	wrk := []string{}

	for i := idx + 1; i < len(os.Args); i++ {
		if os.Args[i] == "---" {
			commands = append(commands, wrk)
			wrk = []string{}
		} else {
			wrk = append(wrk, os.Args[i])
		}
	}

	if len(wrk) > 0 {
		commands = append(commands, wrk)
	}

	if len(commands) < 2 {
		fmt.Println("at least two commands are required")
		os.Exit(1)
	}

	return commands[0], commands[1:]
}

func toCommand(args []string) *exec.Cmd {
	cmd := exec.Command(args[0], args[1:]...)
	return cmd
}

func toCommands(args [][]string) []*exec.Cmd {
	cmds := []*exec.Cmd{}
	for _, arg := range args {
		cmds = append(cmds, toCommand(arg))
	}
	return cmds
}

// createCommandChain creates a chain of commands connected by pipes
// and returns the first and last commands in the chain
func createCommandChain(commands []*exec.Cmd, wg *sync.WaitGroup, rcf func(cmd *exec.Cmd) (io.ReadCloser, error)) (*exec.Cmd, *exec.Cmd) {
	if len(commands) == 0 {
		return nil, nil
	}

	firstCmd := commands[0]
	lastCmd := commands[len(commands)-1]

	for i := 0; i < len(commands)-1; i++ {
		pipe, err := rcf(commands[i])
		if err != nil {
			fmt.Printf("error creating pipe for command %d: %v\n", i, err)
			os.Exit(1)
		}

		pr, pw := io.Pipe()
		commands[i+1].Stdin = pr

		wg.Add(1)
		go func(pipe io.ReadCloser, pw io.WriteCloser) {
			defer wg.Done()
			defer pw.Close()
			io.Copy(pw, pipe)
		}(pipe, pw)
	}

	return firstCmd, lastCmd
}

func main() {
	commandToRun, commandsToPipe := isolateCommands(os.Args)

	command := toCommand(commandToRun)
	// Create all commands
	stdOutCommands := toCommands(commandsToPipe)
	stdErrCommands := toCommands(commandsToPipe)
	// Create a WaitGroup to track all goroutines
	var wg sync.WaitGroup

	// Create two separate chains - one for stdout and one for stderr
	stdoutFirst, stdoutLast := createCommandChain(stdOutCommands, &wg, func(cmd *exec.Cmd) (io.ReadCloser, error) {
		return cmd.StdoutPipe()
	})
	stderrFirst, stderrLast := createCommandChain(stdErrCommands, &wg, func(cmd *exec.Cmd) (io.ReadCloser, error) {
		return cmd.StderrPipe()
	})

	// Set up the final outputs
	if stdoutLast != nil {
		stdoutLast.Stdout = os.Stdout
	}
	if stderrLast != nil {
		stderrLast.Stdout = os.Stderr
	}

	command.Stdin = os.Stdin

	stdOutPipe, err := command.StdoutPipe()
	if err != nil {
		fmt.Printf("error creating stdout pipe for command: %v\n", err)
		os.Exit(1)
	}

	stdErrPipe, err := command.StderrPipe()
	if err != nil {
		fmt.Printf("error creating stderr pipe for command: %v\n", err)
		os.Exit(1)
	}

	stdoutFirst.Stdin = stdOutPipe
	stderrFirst.Stdin = stdErrPipe

	if err := command.Start(); err != nil {
		fmt.Printf("error starting command: %v\n", err)
		os.Exit(1)
	}

	for _, cmd := range stdOutCommands {
		if err := cmd.Start(); err != nil {
			fmt.Printf("error starting command: %v\n", err)
			os.Exit(1)
		}
	}
	for _, cmd := range stdErrCommands {
		if err := cmd.Start(); err != nil {
			fmt.Printf("error starting command: %v\n", err)
			os.Exit(1)
		}
	}

	// Track the exit code
	var exitCode int

	// Wait for all commands to finish
	if err := stdoutFirst.Wait(); err != nil {
		if errd, ok := err.(*exec.ExitError); ok {
			exitCode = errd.ExitCode()
		} else {
			fmt.Printf("error waiting for stdout command: %v\n", err)
			exitCode = 1
		}
	}
	if err := stderrFirst.Wait(); err != nil {
		if errd, ok := err.(*exec.ExitError); ok {
			if errd.ExitCode() != 0 {
				exitCode = errd.ExitCode()
			}
		} else {
			fmt.Printf("error waiting for stderr command: %v\n", err)
			if exitCode == 0 {
				exitCode = 1
			}
		}
	}

	// Wait for all pipe handling goroutines to finish
	wg.Wait()

	// Exit with the appropriate code
	os.Exit(exitCode)
}
