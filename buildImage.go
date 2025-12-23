package main


import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type BuildResult struct {
	ImageID   string
	ImageSize int64 // bytes
	Success   bool
	Error     error
}




func buildDockerImage(buildDir, serviceID string) BuildResult {
	imageTag := "mesh/" + serviceID

	cmd := exec.Command(
		"docker", "build",
		"-t", imageTag,
		buildDir,
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return BuildResult{
			Success: false,
			Error:   fmt.Errorf("docker build failed: %s", stderr.String()),
		}
	}

	// Get image ID
	imageID, err := getImageID(imageTag)
	if err != nil {
		return BuildResult{Success: false, Error: err}
	}

	// Get image size
	size, err := getImageSize(imageID)
	if err != nil {
		return BuildResult{Success: false, Error: err}
	}

	return BuildResult{
		ImageID:   imageID,
		ImageSize: size,
		Success:   true,
	}
}

func getImageID(tag string) (string, error) {
	cmd := exec.Command(
		"docker", "images",
		"--no-trunc",
		"--quiet",
		tag,
	)

	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	imageID := strings.TrimSpace(string(out))
	if imageID == "" {
		return "", fmt.Errorf("image ID not found for tag %s", tag)
	}

	return imageID, nil
}

func getImageSize(imageID string) (int64, error) {
	cmd := exec.Command(
		"docker", "image", "inspect",
		imageID,
		"--format", "{{.Size}}",
	)

	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	var size int64
	if err := json.Unmarshal(out, &size); err != nil {
		return 0, err
	}

	return size, nil
}
