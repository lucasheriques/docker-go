//go:build linux
// +build linux

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

func isolateFilesystem(path string) error {
	tempDir, err := os.MkdirTemp("", "docker")
	if err != nil {
		return fmt.Errorf("Error while creating temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	err = os.MkdirAll(filepath.Join(tempDir, "/dev/null"), 06)
	if err != nil {
		return fmt.Errorf("Err while creating /dev/null: %v", err)
	}

	destinationPath := filepath.Join(tempDir, path)
	if err = os.MkdirAll(filepath.Dir(destinationPath), 0660); err != nil {
		return fmt.Errorf("Err while populating file system: %v", err)
	}

	binary, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("Err while opening binary: %v", err)
	}
	defer binary.Close()

	dest, err := os.Create(destinationPath)
	if err != nil {
		return fmt.Errorf("Err while creating binary path: %v", err)
	}
	defer dest.Close()

	err = dest.Chmod(0100)
	if err != nil {
		return fmt.Errorf("Err while setting chmod perms: %v", err)
	}

	_, err = io.Copy(dest, binary)
	if err != nil {
		return fmt.Errorf("Err while copying file: %v", err)
	}

	if err := syscall.Chroot(tempDir); err != nil {
		return fmt.Errorf("Err while setting chroot: %v", err)
	}

	return nil
}

func handleError(err error) {
	var exitError *exec.ExitError

	if errors.As(err, &exitError) {
		os.Exit(exitError.ExitCode())
	} else {
		fmt.Printf("Err: %v", err)
		os.Exit(1)
	}

}

// Usage: your_docker.sh run <image> <command> <arg1> <arg2> ...
func main() {
	command := os.Args[3]
	args := os.Args[4:len(os.Args)]

	err := isolateFilesystem(command)
	if err != nil {
		handleError(err)
	}

	cmd := exec.Command(command, args...)

	// we need to guard the processs tree so the program we're running
	// is only able to see the process tree that we want it to see.
	// to do that, we'll use PID namespaces to ensure the program
	// has its own process tree. The process being executed must see itself as PID 1.

	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin

	cmd.Env = []string{"PID1=-[ns-process]- # "}

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWPID,
	}

	err = cmd.Run()
	if err != nil {
		handleError(err)
	}

	os.Exit(0)
}
