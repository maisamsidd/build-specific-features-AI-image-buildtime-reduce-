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
		dockerTag := fmt.Sprintf("%s:%s", fname, newHash[:8])
		cmd := exec.Command("docker", "build", "-t", dockerTag, feat.Inputs[0])
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("docker build failed for %s: %w", fname, err)
		}

		cache.WriteFeatureHash(fname, newHash)
		hashCache[fname] = newHash
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
