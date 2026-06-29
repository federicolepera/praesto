package modeldownload

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	praestov1alpha1 "github.com/federicolepera/praesto/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestRouterDelegatesToHuggingFaceDownloader(t *testing.T) {
	downloader := &fakeDownloader{}
	router := &Router{HuggingFace: downloader}
	req := Request{ModelCache: testModelCache(), TargetPath: "/tmp/model"}

	if err := router.Download(context.Background(), req); err != nil {
		t.Fatalf("download: %v", err)
	}
	if !downloader.called {
		t.Fatalf("expected HuggingFace downloader to be called")
	}
}

func TestRouterRequiresHuggingFaceDownloader(t *testing.T) {
	router := &Router{}
	err := router.Download(context.Background(), Request{ModelCache: testModelCache(), TargetPath: "/tmp/model"})
	if err == nil || err.Error() != "huggingface downloader is not configured" {
		t.Fatalf("expected missing downloader error, got %v", err)
	}
}

func TestHuggingFaceClientDownloadsPublicRepoFiles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/models/org/model/tree/main":
			_, _ = w.Write([]byte(`[
				{"type":"directory","path":"nested"},
				{"type":"file","path":"config.json"},
				{"type":"file","path":"nested/model.bin"}
			]`))
		case "/org/model/resolve/main/config.json":
			_, _ = w.Write([]byte(`{"model":"test"}`))
		case "/org/model/resolve/main/nested/model.bin":
			_, _ = w.Write([]byte("weights"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	targetPath := t.TempDir()
	downloader := &HuggingFaceClient{BaseURL: server.URL, HTTPClient: server.Client()}
	if err := downloader.Download(context.Background(), Request{ModelCache: testModelCache(), TargetPath: targetPath}); err != nil {
		t.Fatalf("download: %v", err)
	}

	assertFileContent(t, filepath.Join(targetPath, "config.json"), `{"model":"test"}`)
	assertFileContent(t, filepath.Join(targetPath, "nested", "model.bin"), "weights")
}

type fakeDownloader struct {
	called bool
}

func (f *fakeDownloader) Download(context.Context, Request) error {
	f.called = true
	return nil
}

func testModelCache() *praestov1alpha1.ModelCache {
	return &praestov1alpha1.ModelCache{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: praestov1alpha1.ModelCacheSpec{
			Source:  praestov1alpha1.Source{Huggingface: praestov1alpha1.HuggingfaceSource{Repo: "org/model"}},
			Storage: praestov1alpha1.Storage{Size: "1Gi"},
		},
	}
}

func assertFileContent(t *testing.T, path, expected string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(data) != expected {
		t.Fatalf("unexpected content for %s: %q", path, string(data))
	}
}
