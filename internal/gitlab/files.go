package gitlab

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"

	gitlab "gitlab.com/gitlab-org/api/client-go"
)

// FileContent holds a fetched file's metadata and decoded text content.
type FileContent struct {
	FilePath string
	FileName string
	Ref      string // branch/tag/commit the file was fetched from
	Content  string // decoded UTF-8 text
	Encoding string // raw encoding reported by GitLab (usually "base64")
	Size     int64
}

// GetFileByPath fetches a single file from the repository at the given ref.
//
//	filePath – relative path from the repo root, e.g. "cmd/server/main.go"
//	ref      – branch name, tag, or commit SHA (e.g. "main", "v1.0.0")
func (c *Client) GetFileByPath(filePath, ref string) (*FileContent, error) {
	if ref == "" {
		ref = "main"
	}

	opts := &gitlab.GetFileOptions{Ref: gitlab.Ptr(ref)}
	f, _, err := c.glc.RepositoryFiles.GetFile(c.projectID, filePath, opts)
	if err != nil {
		return nil, fmt.Errorf("gitlab: GetFileByPath %q@%s: %w", filePath, ref, err)
	}

	content, err := decodeFileContent(f.Content, f.Encoding)
	if err != nil {
		return nil, fmt.Errorf("gitlab: GetFileByPath decode %q: %w", filePath, err)
	}

	return &FileContent{
		FilePath: f.FilePath,
		FileName: f.FileName,
		Ref:      f.Ref,
		Content:  content,
		Encoding: f.Encoding,
		Size:     f.Size,
	}, nil
}

// SearchFilesByName searches the repository tree for files matching the given
// filename (case-insensitive substring match on the file name component only).
//
//	filename – file name to search for, e.g. "main.go" or "config"
//	ref      – branch/tag/commit to search in
//
// Returns a slice of matching relative file paths.
func (c *Client) SearchFilesByName(filename, ref string) ([]string, error) {
	if ref == "" {
		ref = "main"
	}

	opts := &gitlab.ListTreeOptions{
		Ref:         gitlab.Ptr(ref),
		Recursive:   gitlab.Ptr(true),
		ListOptions: gitlab.ListOptions{PerPage: 100},
	}

	var matches []string
	lowerTarget := strings.ToLower(filename)

	for {
		nodes, resp, err := c.glc.Repositories.ListTree(c.projectID, opts)
		if err != nil {
			return nil, fmt.Errorf("gitlab: SearchFilesByName %q: %w", filename, err)
		}

		for _, node := range nodes {
			if node.Type == "blob" {
				baseName := strings.ToLower(filepath.Base(node.Path))
				if strings.Contains(baseName, lowerTarget) {
					matches = append(matches, node.Path)
				}
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return matches, nil
}

// GetFilesByName is a convenience method that searches by filename and fetches
// the content of all matching files in one call.
func (c *Client) GetFilesByName(filename, ref string) ([]FileContent, error) {
	paths, err := c.SearchFilesByName(filename, ref)
	if err != nil {
		return nil, err
	}

	var files []FileContent
	for _, p := range paths {
		fc, err := c.GetFileByPath(p, ref)
		if err != nil {
			// Non-fatal — skip files that can't be fetched (e.g. binary files)
			continue
		}
		files = append(files, *fc)
	}
	return files, nil
}

// decodeFileContent handles GitLab's base64-encoded file contents.
func decodeFileContent(raw, encoding string) (string, error) {
	if encoding == "base64" {
		// GitLab wraps lines at 60 chars — strip newlines before decoding.
		cleaned := strings.ReplaceAll(raw, "\n", "")
		b, err := base64.StdEncoding.DecodeString(cleaned)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	// Fallback: return as-is (plain text encoding).
	return raw, nil
}
