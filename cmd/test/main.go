//go:build darwin || dragonfly || freebsd || netbsd || openbsd || hurd
// +build darwin dragonfly freebsd netbsd openbsd hurd

package main

import (
	"fmt"
	"log"
	"os"

	"github.com/mattn/go-isatty"
	"golang.org/x/sys/unix"
)

const ioctlReadTermios = unix.TIOCGETA

func main() {
	fd, err := os.Open("/dev/tty")
	if err != nil {
		log.Fatal(err)
	}
	defer fd.Close()

	res, err := unix.IoctlGetTermios(int(fd.Fd()), ioctlReadTermios)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(res)

	fmt.Println(isatty.IsTerminal(os.Stdout.Fd()))
}
