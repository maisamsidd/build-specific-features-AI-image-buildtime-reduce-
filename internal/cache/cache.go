package cache

import (
	"os"
	"path/filepath"
)

func FeatureHashPath(feature string) string {
	return filepath.Join(".builder-cache", feature, "hash")
}

func ReadFeatureHash(feature string) (string, error) {
	data, err := os.ReadFile(FeatureHashPath(feature))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func WriteFeatureHash(feature, hash string) error {
	dir := filepath.Dir(FeatureHashPath(feature))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(FeatureHashPath(feature), []byte(hash), 0644)
}
