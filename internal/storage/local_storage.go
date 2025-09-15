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
