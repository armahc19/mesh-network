package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	//"strconv"
	"syscall"
	"time"
)

// Define a type for the callback function
type OnSuccess func(imageID string, port string)


type RuntimeInfo struct {
	Runtime string
	Version string
}

type ResourceCheckResult struct {
	CanHostLocally bool   // True if node can run container
	Reason         string // Why/why not
	DiskFree       uint64
	RAMFree        uint64
	CPUCores       int
}

type ResourceCheckResultDHT struct {
	RAMFree   uint64 // bytes
	DiskFree  uint64 // bytes
	CPUCores  int
	CPUArch   string
	GPU       bool
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// -----------------------
// Version Extractors
// -----------------------
func extractGoVersion(goModPath string) string {
	data, err := os.ReadFile(goModPath)
	if err != nil {
		return "default"
	}
	re := regexp.MustCompile(`go (\d+\.\d+)`)
	match := re.FindStringSubmatch(string(data))
	if len(match) > 1 {
		return match[1]
	}
	return "default"
}

func extractNodeVersion(pkgPath string) string {
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return "default"
	}
	var pkg struct {
		Engines map[string]string `json:"engines"`
	}
	json.Unmarshal(data, &pkg)
	if v, ok := pkg.Engines["node"]; ok {
		return v
	}
	return "default"
}

func extractPythonVersion(pyprojectPath, requirementsPath string) string {
	if fileExists(pyprojectPath) {
		data, err := os.ReadFile(pyprojectPath)
		if err == nil {
			re := regexp.MustCompile(`python\s*=\s*["']?(\d+\.\d+).*["']?`)
			m := re.FindStringSubmatch(string(data))
			if len(m) > 1 {
				return m[1]
			}
		}
	}
	if fileExists(requirementsPath) {
		data, err := os.ReadFile(requirementsPath)
		if err == nil {
			re := regexp.MustCompile(`python\s*==\s*(\d+\.\d+)`)
			m := re.FindStringSubmatch(string(data))
			if len(m) > 1 {
				return m[1]
			}
		}
	}
	return "default"
}

// -----------------------
// Runtime Detection
// -----------------------
func detectRuntime(projectPath string) (RuntimeInfo, error) {
	// -----------------------
	// Go
	goMod := filepath.Join(projectPath, "go.mod")
	if fileExists(goMod) || fileExists(filepath.Join(projectPath, "main.go")) || fileExists(filepath.Join(projectPath, "go.sum")) {
		return RuntimeInfo{
			Runtime: "go",
			Version: extractGoVersion(goMod),
		}, nil
	}

	// -----------------------
	// Node.js / React / Vue / Next / Svelte / Angular / TS
	pkgJSON := filepath.Join(projectPath, "package.json")
	if fileExists(pkgJSON) || fileExists(filepath.Join(projectPath, "app.js")) || fileExists(filepath.Join(projectPath, "index.js")) {
		// Distinguish frameworks if needed
		if fileExists(filepath.Join(projectPath, "next.config.js")) {
			return RuntimeInfo{"nextjs", extractNodeVersion(pkgJSON)}, nil
		}
		if fileExists(filepath.Join(projectPath, "vue.config.js")) {
			return RuntimeInfo{"vue", extractNodeVersion(pkgJSON)}, nil
		}
		if fileExists(filepath.Join(projectPath, "svelte.config.js")) {
			return RuntimeInfo{"svelte", extractNodeVersion(pkgJSON)}, nil
		}
		if fileExists(filepath.Join(projectPath, "angular.json")) {
			return RuntimeInfo{"angular", extractNodeVersion(pkgJSON)}, nil
		}
		if fileExists(filepath.Join(projectPath, "tsconfig.json")) {
			return RuntimeInfo{"typescript", extractNodeVersion(pkgJSON)}, nil
		}
		if fileExists(filepath.Join(projectPath, "src/index.js")) || fileExists(filepath.Join(projectPath, "src/App.js")) || fileExists(filepath.Join(projectPath, "public/index.html")) {
			return RuntimeInfo{"react", extractNodeVersion(pkgJSON)}, nil
		}

		return RuntimeInfo{"node", extractNodeVersion(pkgJSON)}, nil
	}

	// -----------------------
	// Python
	pyProj := filepath.Join(projectPath, "pyproject.toml")
	reqs := filepath.Join(projectPath, "requirements.txt")
	if fileExists(pyProj) || fileExists(reqs) || fileExists(filepath.Join(projectPath, "Pipfile")) || fileExists(filepath.Join(projectPath, "setup.py")) {
		return RuntimeInfo{
			Runtime: "python",
			Version: extractPythonVersion(pyProj, reqs),
		}, nil
	}

	// -----------------------
	// Java
	if fileExists(filepath.Join(projectPath, "pom.xml")) || fileExists(filepath.Join(projectPath, "build.gradle")) || fileExists(filepath.Join(projectPath, "build.gradle.kts")) {
		return RuntimeInfo{"java", "default"}, nil
	}

	// -----------------------
	// .NET
	if fileExists(filepath.Join(projectPath, "project.csproj")) || fileExists(filepath.Join(projectPath, "project.fsproj")) || fileExists(filepath.Join(projectPath, "project.vbproj")) {
		return RuntimeInfo{"dotnet", "default"}, nil
	}

	// -----------------------
	// Rust
	if fileExists(filepath.Join(projectPath, "Cargo.toml")) || fileExists(filepath.Join(projectPath, "Cargo.lock")) || fileExists(filepath.Join(projectPath, "main.rs")) {
		return RuntimeInfo{"rust", "default"}, nil
	}

	// -----------------------
	// PHP
	if fileExists(filepath.Join(projectPath, "composer.json")) || fileExists(filepath.Join(projectPath, "composer.lock")) || fileExists(filepath.Join(projectPath, "index.php")) {
		return RuntimeInfo{"php", "default"}, nil
	}

	// -----------------------
	// Ruby
	if fileExists(filepath.Join(projectPath, "Gemfile")) || fileExists(filepath.Join(projectPath, "Gemfile.lock")) || fileExists(filepath.Join(projectPath, "config.ru")) {
		return RuntimeInfo{"ruby", "default"}, nil
	}

	// -----------------------
	// Swift
	if fileExists(filepath.Join(projectPath, "Package.swift")) {
		return RuntimeInfo{"swift", "default"}, nil
	}

	// -----------------------
	// Elixir
	if fileExists(filepath.Join(projectPath, "mix.exs")) {
		return RuntimeInfo{"elixir", "default"}, nil
	}

	// -----------------------
	// Dart / Flutter
	if fileExists(filepath.Join(projectPath, "pubspec.yaml")) {
		return RuntimeInfo{"dart/flutter", "default"}, nil
	}

	// -----------------------
	// Deno / Bun
	if fileExists(filepath.Join(projectPath, "deno.json")) || fileExists(filepath.Join(projectPath, "bun.lockb")) {
		return RuntimeInfo{"deno_bun", "default"}, nil
	}

	// -----------------------
	// Static site
	htmlFiles, _ := filepath.Glob(filepath.Join(projectPath, "*.html"))
	if len(htmlFiles) > 0 {
		return RuntimeInfo{"static", "default"}, nil
	}

	return RuntimeInfo{"unknown", "default"}, errors.New("unsupported project type: could not detect runtime")
}

func writeDockerfile(buildDir string, info RuntimeInfo) error {
	template, err := dockerfileTemplate(info)
	if err != nil {
		return err
	}

	dockerfilePath := filepath.Join(buildDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(template), 0644); err != nil {
		return err
	}

	fmt.Println("✔ Dockerfile generated at", dockerfilePath)
	return nil
}

func checkResources(buildPath string, imageSize uint64) (*ResourceCheckResult, error) {
	// -------- CPU --------
	cpuCores := runtime.NumCPU()
	if cpuCores < 1 {
		cpuCores = 1 // fallback
	}

	// -------- Disk --------
	var fs syscall.Statfs_t
	if err := syscall.Statfs(buildPath, &fs); err != nil {
		return nil, err
	}
	diskFree := fs.Bavail * uint64(fs.Bsize)

	requiredDisk := imageSize * 2 // MVP rule: image + writable layer + temp/logs
	if diskFree < requiredDisk {
		return &ResourceCheckResult{
			CanHostLocally: false,
			Reason:         "Insufficient disk space",
			DiskFree:       diskFree,
			CPUCores:       cpuCores,
		}, nil
	}

	// -------- RAM --------
	var sysinfo syscall.Sysinfo_t
	if err := syscall.Sysinfo(&sysinfo); err != nil {
		return nil, err
	}

	ramFree := (sysinfo.Freeram + sysinfo.Bufferram) * uint64(sysinfo.Unit)

	// MVP rule: minimum 256 MB or image size (whichever is larger)
	requiredRAM := uint64(256 * 1024 * 1024)
	if imageSize > requiredRAM {
		requiredRAM = imageSize
	}

	if ramFree < requiredRAM {
		return &ResourceCheckResult{
			CanHostLocally: false,
			Reason:         "Insufficient RAM",
			DiskFree:       diskFree,
			RAMFree:        ramFree,
			CPUCores:       cpuCores,
		}, nil
	}

	return &ResourceCheckResult{
		CanHostLocally: true,
		Reason:         "Sufficient resources",
		DiskFree:       diskFree,
		RAMFree:        ramFree,
		CPUCores:       cpuCores,
	}, nil
}

// running docker container build
func runContainerCLI(image, hostPort, containerPort string) error {
	cmd := exec.Command(
		"docker", "run", "-d",
		"-p", hostPort+":"+containerPort,
		image,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker run failed: %v, output: %s", err, string(output))
	}

	fmt.Println("✔ Container started locally, ID:", string(output))
	return nil
}


func detect_runtime(pathway string, serviceID string,port int,node *Node) {
	projectPath := pathway // replace with real build path

	fmt.Println("🔍 Detecting project runtime...")

	time.Sleep(10 * time.Second) // Simulate detection delay

	info, err := detectRuntime(projectPath)
	if err != nil {
		fmt.Println("❌ Runtime detection failed:", err)
		return
	}

	fmt.Println("✔ Detected runtime:", info.Runtime, "Version:", info.Version)

	// Generate dockerfile
	fmt.Println("🛠  Generating Dockerfile...")

	time.Sleep(20 * time.Second) // Simulate generation delay

	err = writeDockerfile(projectPath, info)
	if err != nil {
		fmt.Println("❌ Dockerfile generation failed:", err)
		return
	}
	fmt.Println("✔ Dockerfile generation completed.")

	// docker build
	fmt.Println("🛠  Build image...")

	buildResult := buildDockerImage(projectPath, serviceID) // replace with real service ID

	if !buildResult.Success {
		fmt.Println("❌ Image build failed:", buildResult.Error)
		return
	}



	fmt.Println("✔ Image build completed.")

	// hash and size extraction
	fmt.Println("Extracting image details...")
	time.Sleep(5 * time.Second) // Simulate extraction delay

	fmt.Println("Extracting image hash ...")
	time.Sleep(5 * time.Second) // Simulate extraction delay


	fmt.Println("✔ Image hash extracted.")
	fmt.Println("Image hash:", buildResult.ImageID)

	fmt.Println("Extracting image size ...")
	time.Sleep(5 * time.Second) // Simulate extraction delay

	fmt.Println("✔ Image size extracted.")
	fmt.Println("Image size:", buildResult.ImageSize, "bytes")
	time.Sleep(5 * time.Second) // Simulate extraction delay


	fmt.Println("✔ Image details extracted.")

	// Resource check
	fmt.Println("🔍 Checking system resources...")
	time.Sleep(10 * time.Second) // Simulate decision delay

	result, err := checkResources(projectPath, uint64(buildResult.ImageSize))
	if err != nil {
		fmt.Println("❌ Resource check failed:", err)
		return
	}

	fmt.Println("---- Local Resource Check ----")
	fmt.Printf("Can host locally? %v\n", result.CanHostLocally)
	fmt.Printf("Reason: %s\n", result.Reason)
	fmt.Printf("Disk free: %.2f GB\n", float64(result.DiskFree)/(1024*1024*1024))
	fmt.Printf("RAM free: %.2f GB\n", float64(result.RAMFree)/(1024*1024*1024))
	fmt.Printf("CPU cores: %d\n", result.CPUCores)

	// Final decision
	fmt.Println("🔍 Deciding hosting method...")
	time.Sleep(10 * time.Second) // Simulate decision delay

	if result.CanHostLocally {
		fmt.Println("✔ System can host the container locally.")
		fmt.Println("Host: Local")

		fmt.Println("🚀 Starting  container locally")
		time.Sleep(5 * time.Second) // Simulate startup delay

		portStr := fmt.Sprintf("%d", port)

		if err := runContainerCLI(buildResult.ImageID, portStr, portStr); err != nil {
			fmt.Println(err)
		} 

		time.Sleep(5 * time.Second) // Simulate startup delay
		fmt.Println("✔ Container started successfully.")

		// start the service advertisement 
		fmt.Println("Advertising service")
		

		/*// 1. Convert the port string to integer
		pInt, _ := strconv.Atoi(portStr)

		// 2. Call the registration function (from Phase 1 code)
		_, err := RegisterLocalService(h.ID().String(), buildResult.ImageID, pInt)
		if err != nil {
			fmt.Printf("❌ Identity error: %v\n", err)
		} else {
			fmt.Println("📢 Service identity registered. Mesh will advertise automatically.")


		}
		// Optional: Immediate push to DHT
		c, _ := ServiceIDToCID(ServiceID(buildResult.ImageID))
		go kadDHT.Provide(ctx, c, true) */
		fmt.Println("📢 Advertising service to mesh...")
        
        // No need for 'go func' or 'strconv' here since 'port' is already an int
        if err := node.OnServiceStarted(buildResult.ImageID, port); err != nil {
            fmt.Printf("❌ Mesh update failed: %v\n", err)
        } else {
            fmt.Println("✅ Mesh Advertisement Successful")
        }


	} else {
		fmt.Println("❌ System cannot host the container locally:", result.Reason)
		fmt.Println("Host: Seed")
	}
	

}

// CollectRuntimeResources inspects the local machine
func CollectRuntimeResources() *ResourceCheckResultDHT {
	var stat syscall.Sysinfo_t
	syscall.Sysinfo(&stat)

	var fs syscall.Statfs_t
	syscall.Statfs("/", &fs)

	return &ResourceCheckResultDHT{
		RAMFree:  stat.Freeram * uint64(stat.Unit),
		DiskFree: fs.Bavail * uint64(fs.Bsize),
		CPUCores: runtime.NumCPU(),
		CPUArch:  runtime.GOARCH,
		GPU:      hasNvidiaGPU(),
	}
}

func hasNvidiaGPU() bool {
	_, err := os.Stat("/dev/nvidia0")
	return err == nil
}

