package storage

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewLocalStorage(t *testing.T) {
	tempDir := t.TempDir()

	storage, err := NewLocalStorage(tempDir)
	require.NoError(t, err)
	require.NotNil(t, storage)
	require.Equal(t, tempDir, storage.basePath)

	_, err = os.Stat(tempDir)
	require.NoError(t, err, "Base directory should be created")
}

func TestLocalStorage_SaveGetDelete(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewLocalStorage(tempDir)
	require.NoError(t, err)

	id := "test_file_id_12345"
	content := "Hello, world!"
	contentReader := strings.NewReader(content)

	err = storage.Save(id, contentReader)
	require.NoError(t, err)

	expectedPath := storage.getPathFromID(id)
	fileInfo, err := os.Stat(expectedPath)
	require.NoError(t, err, "File should exist after save")
	require.Equal(t, int64(len(content)), fileInfo.Size())

	readCloser, err := storage.Get(id)
	require.NoError(t, err)

	retrievedContent, err := io.ReadAll(readCloser)
	require.NoError(t, err)
	readCloser.Close()
	require.Equal(t, content, string(retrievedContent))

	err = storage.Delete(id)
	require.NoError(t, err)

	_, err = os.Stat(expectedPath)
	require.Error(t, err)
	require.True(t, os.IsNotExist(err), "File should not exist after delete")
}

func TestLocalStorage_GetNonExistent(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewLocalStorage(tempDir)
	require.NoError(t, err)

	_, err = storage.Get("non_existent_id")
	require.Error(t, err)
}

func TestLocalStorage_DeleteNonExistent(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewLocalStorage(tempDir)
	require.NoError(t, err)

	err = storage.Delete("non_existent_id")
	require.NoError(t, err)
}

func TestLocalStorage_SaveWithLargeData(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewLocalStorage(tempDir)
	require.NoError(t, err)

	id := "large_file_id"
	largeContent := make([]byte, 1024*1024)
	for i := range largeContent {
		largeContent[i] = 'a'
	}
	contentReader := bytes.NewReader(largeContent)

	err = storage.Save(id, contentReader)
	require.NoError(t, err)

	expectedPath := storage.getPathFromID(id)
	fileInfo, err := os.Stat(expectedPath)
	require.NoError(t, err)
	require.Equal(t, int64(len(largeContent)), fileInfo.Size())
}

func TestLocalStorage_Copy(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "storage-copy-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	storage, err := NewLocalStorage(tempDir)
	require.NoError(t, err)

	sourceID := "source_file_id_123"
	destID := "dest_file_id_456"
	fileContent := "to jest test kopiowania"

	err = storage.Save(sourceID, strings.NewReader(fileContent))
	require.NoError(t, err)

	err = storage.Copy(sourceID, destID)

	require.NoError(t, err)

	destPath := storage.getPathFromID(destID)

	_, err = os.Stat(destPath)
	require.NoError(t, err, "Destination file should exist after copy")

	copiedContent, err := os.ReadFile(destPath)
	require.NoError(t, err)
	require.Equal(t, fileContent, string(copiedContent))
}

func TestLocalStorage_Copy_SourceNotFound(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "storage-copy-test-notfound")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	storage, err := NewLocalStorage(tempDir)
	require.NoError(t, err)

	err = storage.Copy("non_existent_source", "any_destination")

	require.Error(t, err)
	require.Contains(t, err.Error(), "source file for copy not found")
}
