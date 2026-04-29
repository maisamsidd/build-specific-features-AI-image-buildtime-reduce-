package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/gin-gonic/gin"
	"github.com/maisam9060/platform-api/internal/cache"
	"github.com/maisam9060/platform-api/internal/config"
	"gopkg.in/yaml.v3"
)

// --- Harbor config ---
const (
	harborRegistry = "harbor.qbscocloud.net:30003"
	harborProject  = "verseye-project"
)

// harborImage returns the full Harbor image reference for a feature and hash tag.
// e.g. harbor.qbscocloud.net:30003/verseye-project/feature3:ab12cd34
func harborImage(featureName, tag string) string {
	return fmt.Sprintf("%s/%s/%s:%s", harborRegistry, harborProject, featureName, tag)
}

// harborLogin authenticates docker with Harbor using env vars
// HARBOR_USER and HARBOR_PASSWORD (or HARBOR_TOKEN for robot accounts).
func harborLogin() error {
	user := os.Getenv("HARBOR_USER")
	password := os.Getenv("HARBOR_PASSWORD")
	if password == "" {
		password = os.Getenv("HARBOR_TOKEN") // support robot account tokens
	}
	if user == "" || password == "" {
		return fmt.Errorf("HARBOR_USER and HARBOR_PASSWORD (or HARBOR_TOKEN) env vars must be set")
	}

	r, w, _ := os.Pipe()
	go func() {
		w.WriteString(password)
		w.Close()
	}()

	cmd := exec.Command("docker", "login", harborRegistry, "-u", user, "--password-stdin")
	cmd.Stdin = r
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker login to Harbor failed: %w", err)
	}
	fmt.Printf("Logged in to Harbor: %s\n", harborRegistry)
	return nil
}

// --- Hashing ---
func HashDir(dir string) (string, error) {
	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}

	sort.Strings(files)
	h := sha256.New()
	for _, f := range files {
		file, err := os.Open(f)
		if err != nil {
			return "", err
		}
		defer file.Close()
		if _, err := io.Copy(h, file); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func ComputeFeatureHash(feat *config.Feature, depHashes []string) (string, error) {
	h := sha256.New()
	h.Write([]byte(feat.Command))
	for _, input := range feat.Inputs {
		hash, err := HashDir(input)
		if err != nil {
			return "", err
		}
		h.Write([]byte(hash))
	}
	sort.Strings(depHashes)
	for _, dh := range depHashes {
		h.Write([]byte(dh))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// --- Dependency graph ---
func buildGraph(cfg *config.BuilderConfig) map[string][]string {
	graph := make(map[string][]string)
	for name, feat := range cfg.Features {
		graph[name] = feat.DependsOn
	}
	return graph
}

func topoSort(node string, graph map[string][]string, visited, temp map[string]bool, order *[]string) error {
	if temp[node] {
		return fmt.Errorf("cycle detected at feature: %s", node)
	}
	if visited[node] {
		return nil
	}
	temp[node] = true
	for _, dep := range graph[node] {
		if _, ok := graph[dep]; !ok {
			return fmt.Errorf("unknown dependency: %s", dep)
		}
		if err := topoSort(dep, graph, visited, temp, order); err != nil {
			return err
		}
	}
	temp[node] = false
	visited[node] = true
	*order = append(*order, node)
	return nil
}

// --- Build logic ---
func BuildFeature(featureName string) error {
	// Load YAML
	data, err := os.ReadFile("builder.yaml")
	if err != nil {
		return fmt.Errorf("reading YAML: %w", err)
	}

	var cfg config.BuilderConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parsing YAML: %w", err)
	}

	// Attach names
	for name, feat := range cfg.Features {
		feat.Name = name
	}

	graph := buildGraph(&cfg)
	visited := make(map[string]bool)
	temp := make(map[string]bool)
	var buildOrder []string
	if err := topoSort(featureName, graph, visited, temp, &buildOrder); err != nil {
		return fmt.Errorf("dependency error: %w", err)
	}

	fmt.Println("Build order:", buildOrder)

	// Login to Harbor once before any builds
	if err := harborLogin(); err != nil {
		return err
	}

	hashCache := make(map[string]string)
	for _, fname := range buildOrder {
		feat := cfg.Features[fname]

		// Collect dependency hashes
		var depHashes []string
		for _, dep := range feat.DependsOn {
			depHashes = append(depHashes, hashCache[dep])
		}

		// Compute current feature hash
		newHash, err := ComputeFeatureHash(feat, depHashes)
		if err != nil {
			return fmt.Errorf("hashing feature %s: %w", fname, err)
		}

		oldHash, err := cache.ReadFeatureHash(fname)
		if err == nil && oldHash == newHash {
			fmt.Println("SKIP", fname)
			hashCache[fname] = newHash
			continue
		}

		fmt.Println("BUILD", fname)

		shortHash := newHash[:8]
		localTag := fmt.Sprintf("%s:%s", fname, shortHash)
		remoteTag := harborImage(fname, shortHash)

		// 1. docker build — tag locally
		buildCmd := exec.Command("docker", "build", "-t", localTag, feat.Inputs[0])
		buildCmd.Stdout = os.Stdout
		buildCmd.Stderr = os.Stderr
		if err := buildCmd.Run(); err != nil {
			return fmt.Errorf("docker build failed for %s: %w", fname, err)
		}

		// 2. docker tag — apply the full Harbor image reference
		tagCmd := exec.Command("docker", "tag", localTag, remoteTag)
		tagCmd.Stdout = os.Stdout
		tagCmd.Stderr = os.Stderr
		if err := tagCmd.Run(); err != nil {
			return fmt.Errorf("docker tag failed for %s: %w", fname, err)
		}
		fmt.Printf("Tagged: %s -> %s\n", localTag, remoteTag)

		// 3. docker push — push to Harbor
		pushCmd := exec.Command("docker", "push", remoteTag)
		pushCmd.Stdout = os.Stdout
		pushCmd.Stderr = os.Stderr
		if err := pushCmd.Run(); err != nil {
			return fmt.Errorf("docker push failed for %s: %w", fname, err)
		}
		fmt.Printf("Pushed to Harbor: %s\n", remoteTag)

		// 4. Update cache only after a successful push
		cache.WriteFeatureHash(fname, newHash)
		hashCache[fname] = newHash

		// 5. docker run — start a container from the Harbor image
		// containerName := fmt.Sprintf("%s_container", fname)
		// fmt.Println("Starting container:", containerName)
		// runCmd := exec.Command("docker", "run", "-d", "--name", containerName, remoteTag)
		// runCmd.Stdout = os.Stdout
		// runCmd.Stderr = os.Stderr
		// if err := runCmd.Run(); err != nil {
		// 	return fmt.Errorf("docker run failed for %s: %w", fname, err)
		// }
		// fmt.Printf("Container started: %s (image: %s)\n", containerName, remoteTag)
	}
	return nil
}

// --- REST API ---
func main() {
	r := gin.Default()
	r.POST("/build", func(c *gin.Context) {
		var req struct {
			Feature string `json:"feature"`
		}
		if err := c.BindJSON(&req); err != nil || req.Feature == "" {
			c.JSON(400, gin.H{"error": "feature is required"})
			return
		}

		if err := BuildFeature(req.Feature); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		c.JSON(200, gin.H{"status": "success", "feature": req.Feature})
	})

	r.Run(":8080")
}
