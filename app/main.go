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
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin

	err = cmd.Run()
	if err != nil {
		handleError(err)
	}

	os.Exit(0)
}
