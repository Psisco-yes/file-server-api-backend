package storage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type LocalStorage struct {
	basePath string
}

func NewLocalStorage(basePath string) (*LocalStorage, error) {
	if err := os.MkdirAll(basePath, os.ModePerm); err != nil {
		return nil, err
	}
	return &LocalStorage{basePath: basePath}, nil
}

func (ls *LocalStorage) getPathFromID(id string) string {
	pathParts := strings.Split(id, "")
	return filepath.Join(ls.basePath, filepath.Join(pathParts...))
}

func (ls *LocalStorage) Save(id string, data io.Reader) error {
	filePath := ls.getPathFromID(id)
	dir := filepath.Dir(filePath)

	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return err
	}

	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, data)
	return err
}

func (ls *LocalStorage) Get(id string) (io.ReadCloser, error) {
	filePath := ls.getPathFromID(id)

	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file with id %s not found: %w", id, err)
		}
		return nil, err
	}

	return file, nil
}

func (ls *LocalStorage) Delete(id string) error {
	filePath := ls.getPathFromID(id)

	err := os.Remove(filePath)
	if os.IsNotExist(err) {
		return nil
	}

	return err
}

func (ls *LocalStorage) Copy(sourceID string, destID string) error {
	sourcePath := ls.getPathFromID(sourceID)
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("source file for copy not found: %w", err)
	}
	defer sourceFile.Close()

	destPath := ls.getPathFromID(destID)
	destDir := filepath.Dir(destPath)

	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return fmt.Errorf("could not create destination directory for copy: %w", err)
	}

	destFile, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("could not create destination file for copy: %w", err)
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return fmt.Errorf("failed to copy file content: %w", err)
	}

	return nil
}
