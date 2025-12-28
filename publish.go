package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
    "errors"
	"net/http"
	"net/url"
    "time"
	"crypto/sha256"
	"encoding/hex"
	"os/exec"
	"path/filepath"
	"io"
)



type PublishRequest struct {
	Source string
	Port   int
}

func validateSource(source string) error {
	// 1️⃣ Check local path
	info, err := os.Stat(source)
	if err == nil && info.IsDir() {
		return nil
	}

	// 2️⃣ Check URL format
	parsed, err := url.Parse(source)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return errors.New("source must be an existing folder or a valid URL")
	}

	if !strings.HasPrefix(parsed.Scheme, "http") {
		return errors.New("only http/https URLs are supported")
	}

	// 3️⃣ Check URL reachability (HEAD request)
	client := http.Client{
		Timeout: 5 * time.Second,
	}

	resp, err := client.Head(source)
	if err != nil {
		return errors.New("cannot reach repository URL")
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return errors.New("repository URL returned error")
	}

	return nil
}

func generateServiceID(source string) string {
	h := sha256.New()
	h.Write([]byte(source))
	return hex.EncodeToString(h.Sum(nil))[:12] // short 12-char hash for folder
}


func copyFolder(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		targetPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(targetPath, info.Mode())
		}

		// Copy file
		sourceFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer sourceFile.Close()

		destFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY, info.Mode())
		if err != nil {
			return err
		}
		defer destFile.Close()

		_, err = io.Copy(destFile, sourceFile)
		return err
	})
}

func cloneRepo(url, dst string) error {
	cmd := exec.Command("git", "clone", "--depth", "1", url, dst)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func handleSource(source string) (string, string, error) {
	serviceID := generateServiceID(source)
	buildDir := filepath.Join(os.Getenv("HOME"), ".mesh", "builds", serviceID)

	// Ensure build directory exists
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		return "","", err
	}

	// Check if local folder
	info, err := os.Stat(source)
	if err == nil && info.IsDir() {
		// Copy local folder
		err = copyFolder(source, buildDir)
		if err != nil {
			return "","", err
		}
		fmt.Println("✔ Local folder copied to build directory:", buildDir)
	} else {
		// Assume Git repo
		err = cloneRepo(source, buildDir)
		if err != nil {
			return "","", err
		}
		fmt.Println("✔ Repo cloned to build directory:", buildDir)
	}

	return buildDir,serviceID, nil
}


func checkDocker() bool {
	// Try to run `docker --version` to see if Docker CLI is available
	cmd := exec.Command("docker", "--version")
	err := cmd.Run()
	return err == nil
}


func publishFlow(scanner *bufio.Scanner,node *Node) {
	fmt.Print("Enter repo URL or folder path: ")
	scanner.Scan()
	path := strings.TrimSpace(scanner.Text())

	if err := validateSource(path); err != nil {
        fmt.Println("Error:", err)
        return
    }    

    // Print which type it is
    info, _ := os.Stat(path)
    if info != nil && info.IsDir() {
        fmt.Println("✔ Local folder exists:", path)
    } else {
        fmt.Println("✔ Repository URL reachable:", path)
    }

	

	fmt.Print("Enter exposed port (e.g., 8080): ")
	scanner.Scan()
	portStr := strings.TrimSpace(scanner.Text())

	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		fmt.Println("Error: invalid port (1–65535).")
		return
	}

	

	req := PublishRequest{
		Source: path,
		Port:   port,
	}

	fmt.Println("\n✔ Publish request created")
	fmt.Printf("Source: %s\nPort: %d\n", req.Source, req.Port)

	fmt.Println("- Source validation")

	buildPath,serviceID ,err := handleSource(path)
	if err != nil {
		fmt.Println("❌ Error handling source:", err)
		return
	}

	fmt.Println("Build directory ready at:", buildPath)
    fmt.Println("Service ID generated:", serviceID)

	
	fmt.Println("- Build job creation")

	if !checkDocker() {
		fmt.Println("❌ Docker CLI not found. Please install Docker to proceed.")
		return
	} 
		fmt.Println("✔ Docker is installed, ready to build containers")
		fmt.Println("Building Docker image...")

		


	fmt.Println("- Containerization")
	detect_runtime(node.Ctx,buildPath, serviceID,port,node)

}



/*func main() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024), 1024*1024)

	for {
		fmt.Println("\nMesh CLI")
		fmt.Println("1. Publish")
		fmt.Println("2. Exit")
		fmt.Print("Enter your choice: ")

		scanner.Scan()
		choice, err := strconv.Atoi(strings.TrimSpace(scanner.Text()))
		if err != nil {
			fmt.Println("Invalid input.")
			continue
		}

		switch choice {
		case 1:
			publishFlow(scanner)
		case 2:
			fmt.Println("Goodbye.")
			return
		default:
			fmt.Println("Invalid choice.")
		}
	}
}
*/