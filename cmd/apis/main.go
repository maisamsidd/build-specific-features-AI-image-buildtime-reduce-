package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"crypto/sha256"
	"encoding/hex"

	"github.com/maisam9060/platform-api/internal/cache"
	"github.com/maisam9060/platform-api/internal/config"
	"gopkg.in/yaml.v3"
)

// HashDir recursively hashes all files in a directory
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

// ComputeFeatureHash computes a hash of feature inputs and dependencies
func ComputeFeatureHash(feat *config.Feature, depHashes []string) (string, error) {
	h := sha256.New()

	// Hash feature command
	h.Write([]byte(feat.Command))

	// Hash all input directories
	for _, input := range feat.Inputs {
		hash, err := HashDir(input)
		if err != nil {
			return "", err
		}
		h.Write([]byte(hash))
	}

	// Hash dependency hashes (sorted)
	sort.Strings(depHashes)
	for _, dh := range depHashes {
		h.Write([]byte(dh))
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func buildGraph(cfg *config.BuilderConfig) map[string][]string {
	graph := make(map[string][]string)

	for name, feat := range cfg.Features {
		graph[name] = feat.DependsOn
	}

	return graph
}

func topoSort(
	node string,
	graph map[string][]string,
	visited map[string]bool,
	temp map[string]bool,
	order *[]string,
) error {

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

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: builder <command> <feature>")
		os.Exit(1)
	}

	// command := os.Args[1]
	featureName := os.Args[2]

	// Load YAML
	data, err := ioutil.ReadFile("builder.yaml")
	if err != nil {
		fmt.Println("Error reading builder.yaml:", err)
		os.Exit(1)
	}

	var cfg config.BuilderConfig
	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		fmt.Println("Error parsing builder.yaml:", err)
		os.Exit(1)
	}

	// Attach names
	for name, feat := range cfg.Features {
		feat.Name = name
	}

	// // Validate feature
	// feat, ok := cfg.Features[featureName]
	// if !ok {
	// 	fmt.Println("Feature not found:", featureName)
	// 	os.Exit(1)
	// }

	// --- Step 4: Topo sort and build features in order ---
	graph := buildGraph(&cfg)

	visited := make(map[string]bool)
	temp := make(map[string]bool)
	var buildOrder []string

	err = topoSort(featureName, graph, visited, temp, &buildOrder)
	if err != nil {
		fmt.Println("Dependency error:", err)
		os.Exit(1)
	}

	fmt.Println("Build order:", buildOrder)

	// Build features in order
	hashCache := make(map[string]string) // cache of this run
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
			fmt.Printf("Error hashing feature %s: %v\n", fname, err)
			os.Exit(1)
		}

		oldHash, err := cache.ReadFeatureHash(fname)
		if err == nil && oldHash == newHash {
			fmt.Println("SKIP", fname)
			hashCache[fname] = newHash
			continue
		}

		fmt.Println("BUILD", fname)

		// --- Docker build ---
		dockerTag := fmt.Sprintf("%s:%s", fname, newHash[:8]) // short hash
		cmd := exec.Command("docker", "build", "-t", dockerTag, feat.Inputs[0])
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			fmt.Printf("Docker build failed for %s: %v\n", fname, err)
			os.Exit(1)
		}

		// Write hash cache
		cache.WriteFeatureHash(fname, newHash)
		hashCache[fname] = newHash
	}

}
