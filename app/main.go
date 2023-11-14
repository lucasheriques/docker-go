//go:build linux
// +build linux

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

type Manifests struct {
	Manifests []struct {
		Digest    string `json:"digest"`
		MediaType string `json:"mediaType"`
		Platform  struct {
			Architecture string `json:"architecture"`
			Os           string `json:"os"`
		} `json:"platform,omitempty"`
		Size int `json:"size"`
	} `json:"manifests"`
	MediaType     string `json:"mediaType"`
	SchemaVersion int    `json:"schemaVersion"`
}

type Manifest struct {
	SchemaVersion int    `json:"schemaVersion"`
	MediaType     string `json:"mediaType"`
	Config        struct {
		MediaType string `json:"mediaType"`
		Size      int    `json:"size"`
		Digest    string `json:"digest"`
	} `json:"config"`
	Layers []struct {
		MediaType string `json:"mediaType"`
		Size      int    `json:"size"`
		Digest    string `json:"digest"`
	} `json:"layers"`
}

const (
	getTokenURL       = "https://auth.docker.io/token?service=registry.docker.io&scope=repository:library/%s:pull"
	getManifestURL    = "https://registry.hub.docker.com/v2/library/%s/manifests/%s"
	getLayerURL       = "https://registry.hub.docker.com/v2/library/%s/blobs/%s"
	contentTypeHeader = "application/vnd.docker.distribution.manifest.v2+json"
	imageFileName     = "image.tar"
	tempDir           = "my-docker"
)

func isolateFilesystem(tempDir string) error {
	err := os.MkdirAll(filepath.Join(tempDir, "/dev/null"), 06)
	if err != nil {
		return fmt.Errorf("Err while creating /dev/null: %v", err)
	}

	if err := syscall.Chroot(tempDir); err != nil {
		return fmt.Errorf("Err while setting chroot: %v", err)
	}

	return nil
}

func handleError(msg string, err error) {
	var exitError *exec.ExitError

	if errors.As(err, &exitError) {
		os.Exit(exitError.ExitCode())
	} else {
		fmt.Printf("%v: %v", msg, err)
		os.Exit(1)
	}
}

func getRegistryAuthToken(image string) string {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf(getTokenURL, image), nil)
	if err != nil {
		handleError("Error when creating request for auth token ", err)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("client: error making http request for auth token: %s\n", err)
		os.Exit(1)
	}

	defer res.Body.Close()
	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		fmt.Printf("client: could not read response body: %s\n", err)
		os.Exit(1)
	}

	var tokenResponse struct {
		Token string `json:"token"`
	}

	if err := json.Unmarshal(resBody, &tokenResponse); err != nil {
		fmt.Printf("client: could not unmarshal response body: %s\n", err)
		os.Exit(1)
	}

	// fmt.Println(fmt.Sprintf("token: %s", tokenResponse.Token))

	return tokenResponse.Token
}

func getImageManifest(token, image, version string) *Manifest {
	// fmt.Println(fmt.Sprintf(getManifestURL, image, version))
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf(getManifestURL, image, version), nil)
	if err != nil {
		handleError("Error when creating request", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	req.Header.Set("Accept", contentTypeHeader)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		handleError("Error when executing image manifest request", err)
	}

	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		handleError("Error when parsing response body", err)
	}
	defer res.Body.Close()

	// fmt.Printf("resBody: %s\n", resBody)

	var manifest Manifest

	if err := json.Unmarshal(resBody, &manifest); err != nil {
		handleError("Error when parsing JSON response for image manifest", err)
	}

	return &manifest
}

func pullDockerLayers(token, image, digest string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf(getLayerURL, image, digest), nil)
	if err != nil {
		handleError("Error when creating request to get layer", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		handleError("Error when executing layer request", err)
	}
	defer res.Body.Close()

	layerFile, err := os.Create(fmt.Sprintf("%s.tar.gz", digest[7:]))
	if err != nil {
		handleError("Error when creating file", err)
	}
	defer layerFile.Close()

	_, err = io.Copy(layerFile, res.Body)
	if err != nil {
		handleError("Error when writing layer to file", err)
	}

	return layerFile.Name(), nil
}

func getImageFromRegistry(image, version, path string) {
	// first we need to get the auth token to make calls to the registry
	token := getRegistryAuthToken(image)

	manifest := getImageManifest(token, image, version)

	layerNames := []string{}
	for _, manifest := range manifest.Layers {
		layerName, err := pullDockerLayers(token, image, manifest.Digest)
		if err != nil {
			handleError("Error when pulling layer", err)
		}

		layerNames = append(layerNames, layerName)
	}

	// printCurrentFilesInDir()

	// fmt.Printf("Layer names: %v\n", layerNames)

	// fmt.Printf("Path: %s\n", path)

	// check permissions for target directory
	err := os.MkdirAll(path, 0777)
	if err != nil {
		handleError("Error when creating directory", err)
	}

	for _, layerName := range layerNames {
		// fmt.Printf("Layer name: %s\n", layerName)

		_, err := os.Stat(layerName)
		if err != nil {
			fmt.Println("Error when getting file info:", err)
			return
		}

		// fmt.Println("File permissions:", info.Mode())

		err = extractTar(layerName, path)
		if err != nil {
			handleError("Error when extracting tar", err)
		}
	}

	// printCurrentFilesInDir()
}

func printCurrentFilesInDir() {
	files, err := os.ReadDir(".")
	if err != nil {
		handleError("Error when reading directory", err)
	}

	fmt.Println("\n\n****** CURRENT FILES ******")

	for _, file := range files {
		fmt.Println(file.Name())
	}

	fmt.Println("****** END FILES ******\n")
}

func extractTar(src, dest string) error {
	cmd := exec.Command("tar", "-xzvf", src, "-C", dest)

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("Error when extracting tar: %v", err)
	}

	return nil
}

// Usage: your_docker.sh run <image> <command> <arg1> <arg2> ...
func main() {
	imageAndVersion := strings.Split(os.Args[2], ":")
	command := os.Args[3]
	args := os.Args[4:len(os.Args)]

	image := imageAndVersion[0]
	version := "latest"

	if len(imageAndVersion) == 2 {
		version = imageAndVersion[1]
	}

	tempDir, err := os.MkdirTemp("", tempDir)
	if err != nil {
		fmt.Printf("Error creating temporary directory: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tempDir)

	getImageFromRegistry(image, version, tempDir)

	err = isolateFilesystem(tempDir)
	if err != nil {
		handleError("Error when isolating filesystem", err)
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
		handleError("Error when executing command", err)
	}

	os.Exit(0)
}
