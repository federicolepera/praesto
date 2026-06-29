package modeldownload

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

const DefaultHuggingFaceBaseURL = "https://huggingface.co"

const downloadProgressInterval = 10 * time.Second

type HuggingFaceDownloader struct{}

type HuggingFaceClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

var _ Downloader = (*HuggingFaceDownloader)(nil)
var _ Downloader = (*HuggingFaceClient)(nil)

func (d *HuggingFaceDownloader) Download(ctx context.Context, req Request) error {
	return (&HuggingFaceClient{}).Download(ctx, req)
}

func (c *HuggingFaceClient) Download(ctx context.Context, req Request) error {
	if req.ModelCache == nil {
		return fmt.Errorf("model cache is required")
	}
	if strings.TrimSpace(req.TargetPath) == "" {
		return fmt.Errorf("target path is required")
	}

	source := req.ModelCache.Spec.Source.Huggingface
	repo := strings.TrimSpace(source.Repo)
	if repo == "" {
		return fmt.Errorf("huggingface repo is required")
	}
	if source.Token != nil {
		return fmt.Errorf("huggingface token download is not supported by node-agent yet")
	}
	revision := strings.TrimSpace(source.Revision)
	if revision == "" {
		revision = "main"
	}

	files, err := c.listFiles(ctx, repo, revision)
	if err != nil {
		return err
	}
	log.FromContext(ctx).Info("starting Hugging Face model download", "repo", repo, "revision", revision, "files", len(files), "targetPath", req.TargetPath)
	if err := os.MkdirAll(req.TargetPath, 0o775); err != nil {
		return fmt.Errorf("create target path %q: %w", req.TargetPath, err)
	}
	for _, file := range files {
		if err := c.downloadFile(ctx, repo, revision, file, req.TargetPath); err != nil {
			return err
		}
	}
	log.FromContext(ctx).Info("completed Hugging Face model download", "repo", repo, "revision", revision, "files", len(files), "targetPath", req.TargetPath)
	return nil
}

type huggingFaceTreeEntry struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

func (c *HuggingFaceClient) listFiles(ctx context.Context, repo, revision string) ([]string, error) {
	requestURL := c.baseURL() + "/api/models/" + escapePath(repo) + "/tree/" + url.PathEscape(revision) + "?recursive=1"
	resp, err := c.do(ctx, http.MethodGet, requestURL)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list huggingface repo %s@%s: unexpected status %s", repo, revision, resp.Status)
	}

	var entries []huggingFaceTreeEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("decode huggingface tree response: %w", err)
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Type == "file" && cleanRelativePath(entry.Path) != "" {
			files = append(files, entry.Path)
		}
	}
	return files, nil
}

func (c *HuggingFaceClient) downloadFile(ctx context.Context, repo, revision, filePath, targetPath string) error {
	relativePath := cleanRelativePath(filePath)
	if relativePath == "" {
		return fmt.Errorf("invalid huggingface file path %q", filePath)
	}

	destination := filepath.Join(targetPath, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(destination), 0o775); err != nil {
		return fmt.Errorf("create destination directory for %q: %w", destination, err)
	}

	requestURL := c.baseURL() + "/" + escapePath(repo) + "/resolve/" + url.PathEscape(revision) + "/" + escapePath(relativePath)
	resp, err := c.do(ctx, http.MethodGet, requestURL)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download huggingface file %s: unexpected status %s", relativePath, resp.Status)
	}
	contentLength := resp.ContentLength
	log.FromContext(ctx).Info("downloading Hugging Face file", "file", relativePath, "sizeBytes", contentLength, "destination", destination)

	out, err := os.Create(destination)
	if err != nil {
		return fmt.Errorf("create destination file %q: %w", destination, err)
	}
	written, err := copyWithProgress(ctx, out, resp.Body, relativePath, contentLength)
	if err != nil {
		_ = out.Close()
		return fmt.Errorf("write destination file %q: %w", destination, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close destination file %q: %w", destination, err)
	}
	log.FromContext(ctx).Info("downloaded Hugging Face file", "file", relativePath, "writtenBytes", written, "destination", destination)
	return nil
}

func copyWithProgress(ctx context.Context, dst io.Writer, src io.Reader, fileName string, totalBytes int64) (int64, error) {
	buffer := make([]byte, 1024*1024)
	var written int64
	lastLog := time.Now()
	for {
		n, readErr := src.Read(buffer)
		if n > 0 {
			writtenN, writeErr := dst.Write(buffer[:n])
			written += int64(writtenN)
			if writeErr != nil {
				return written, writeErr
			}
			if writtenN != n {
				return written, io.ErrShortWrite
			}
			if time.Since(lastLog) >= downloadProgressInterval {
				log.FromContext(ctx).Info("Hugging Face file download progress", "file", fileName, "writtenBytes", written, "totalBytes", totalBytes)
				lastLog = time.Now()
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				return written, nil
			}
			return written, readErr
		}
	}
}

func (c *HuggingFaceClient) do(ctx context.Context, method, requestURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, requestURL, nil)
	if err != nil {
		return nil, err
	}
	return c.httpClient().Do(req)
}

func (c *HuggingFaceClient) baseURL() string {
	if strings.TrimSpace(c.BaseURL) == "" {
		return DefaultHuggingFaceBaseURL
	}
	return strings.TrimRight(c.BaseURL, "/")
}

func (c *HuggingFaceClient) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func escapePath(path string) string {
	parts := strings.Split(path, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

func cleanRelativePath(path string) string {
	cleaned := filepath.ToSlash(filepath.Clean(path))
	if cleaned == "." || strings.HasPrefix(cleaned, "../") || cleaned == ".." || strings.HasPrefix(cleaned, "/") {
		return ""
	}
	return cleaned
}
