package lfs_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	backendlfs "github.com/wzshiming/gitd/pkg/backend/lfs"
	"github.com/wzshiming/gitd/pkg/storage"
)

const metaMediaType = "application/vnd.git-lfs+json"

func newLFSTestServer(t *testing.T) (*httptest.Server, *storage.Storage) {
	t.Helper()
	dataDir, err := os.MkdirTemp("", "lfs-test-data")
	if err != nil {
		t.Fatalf("Failed to create temp data dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dataDir) })

	stor := storage.NewStorage(storage.WithRootDir(dataDir))
	handler := backendlfs.NewHandler(backendlfs.WithStorage(stor))
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, stor
}

// sha256Hex returns the hex-encoded SHA-256 hash of data.
func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func TestLFSBatchUpload(t *testing.T) {
	srv, _ := newLFSTestServer(t)

	content := []byte("hello lfs content")
	oid := sha256Hex(content)

	batchBody := fmt.Sprintf(`{
		"operation": "upload",
		"objects": [{"oid": %q, "size": %d}]
	}`, oid, len(content))

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/myrepo.git/info/lfs/objects/batch",
		bytes.NewBufferString(batchBody))
	req.Header.Set("Accept", metaMediaType)
	req.Header.Set("Content-Type", metaMediaType)
	req.Host = "localhost"

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("LFS batch request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 OK, got %d", resp.StatusCode)
	}

	var batchResp struct {
		Objects []struct {
			Oid     string                     `json:"oid"`
			Actions map[string]json.RawMessage `json:"actions"`
			Error   *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error,omitempty"`
		} `json:"objects"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&batchResp); err != nil {
		t.Fatalf("Failed to decode batch response: %v", err)
	}

	if len(batchResp.Objects) != 1 {
		t.Fatalf("Expected 1 object in batch response, got %d", len(batchResp.Objects))
	}
	obj := batchResp.Objects[0]
	if obj.Oid != oid {
		t.Errorf("Expected oid %q, got %q", oid, obj.Oid)
	}
	if _, ok := obj.Actions["upload"]; !ok {
		t.Error("Expected 'upload' action for new object")
	}
}

func TestLFSPutAndGetContent(t *testing.T) {
	srv, _ := newLFSTestServer(t)

	content := []byte("lfs object content for put/get test")
	oid := sha256Hex(content)

	// PUT the content
	putReq, _ := http.NewRequest(http.MethodPut, srv.URL+"/objects/"+oid, bytes.NewReader(content))
	putReq.ContentLength = int64(len(content))
	putResp, err := http.DefaultClient.Do(putReq)
	if err != nil {
		t.Fatalf("PUT /objects/%s failed: %v", oid, err)
	}
	putResp.Body.Close()
	if putResp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 OK for PUT, got %d", putResp.StatusCode)
	}

	// GET the content back
	getResp, err := http.Get(srv.URL + "/objects/" + oid)
	if err != nil {
		t.Fatalf("GET /objects/%s failed: %v", oid, err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 OK for GET, got %d", getResp.StatusCode)
	}

	got, err := io.ReadAll(getResp.Body)
	if err != nil {
		t.Fatalf("Failed to read GET response body: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("Content mismatch: got %q, want %q", got, content)
	}
}

func TestLFSGetContentNotFound(t *testing.T) {
	srv, _ := newLFSTestServer(t)

	fakeOid := sha256Hex([]byte("nonexistent"))
	resp, err := http.Get(srv.URL + "/objects/" + fakeOid)
	if err != nil {
		t.Fatalf("GET /objects/%s failed: %v", fakeOid, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404 Not Found, got %d", resp.StatusCode)
	}
}

func TestLFSVerifyObject(t *testing.T) {
	srv, _ := newLFSTestServer(t)

	content := []byte("verify test content")
	oid := sha256Hex(content)

	// Upload the content first
	putReq, _ := http.NewRequest(http.MethodPut, srv.URL+"/objects/"+oid, bytes.NewReader(content))
	putReq.ContentLength = int64(len(content))
	putResp, err := http.DefaultClient.Do(putReq)
	if err != nil {
		t.Fatalf("PUT /objects/%s failed: %v", oid, err)
	}
	putResp.Body.Close()

	// Verify with correct size
	verifyBody := fmt.Sprintf(`{"oid": %q, "size": %d}`, oid, len(content))
	verifyReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/objects/"+oid+"/verify",
		bytes.NewBufferString(verifyBody))
	verifyReq.Header.Set("Content-Type", "application/json")
	verifyResp, err := http.DefaultClient.Do(verifyReq)
	if err != nil {
		t.Fatalf("POST /objects/%s/verify failed: %v", oid, err)
	}
	defer verifyResp.Body.Close()
	if verifyResp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 OK for verify, got %d", verifyResp.StatusCode)
	}
}

func TestLFSBatchDownloadExistingObject(t *testing.T) {
	srv, _ := newLFSTestServer(t)

	content := []byte("existing lfs object")
	oid := sha256Hex(content)

	// Upload the content first
	putReq, _ := http.NewRequest(http.MethodPut, srv.URL+"/objects/"+oid, bytes.NewReader(content))
	putReq.ContentLength = int64(len(content))
	putResp, err := http.DefaultClient.Do(putReq)
	if err != nil {
		t.Fatalf("PUT failed: %v", err)
	}
	putResp.Body.Close()

	// Batch download
	batchBody := fmt.Sprintf(`{
		"operation": "download",
		"objects": [{"oid": %q, "size": %d}]
	}`, oid, len(content))

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/myrepo.git/info/lfs/objects/batch",
		bytes.NewBufferString(batchBody))
	req.Header.Set("Accept", metaMediaType)
	req.Header.Set("Content-Type", metaMediaType)
	req.Host = "localhost"

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("LFS batch request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 OK, got %d", resp.StatusCode)
	}

	var batchResp struct {
		Objects []struct {
			Oid     string                     `json:"oid"`
			Actions map[string]json.RawMessage `json:"actions"`
		} `json:"objects"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&batchResp); err != nil {
		t.Fatalf("Failed to decode batch response: %v", err)
	}

	if len(batchResp.Objects) != 1 {
		t.Fatalf("Expected 1 object, got %d", len(batchResp.Objects))
	}
	if _, ok := batchResp.Objects[0].Actions["download"]; !ok {
		t.Error("Expected 'download' action for existing object")
	}
}

func TestLFSLockCreateAndList(t *testing.T) {
	srv, _ := newLFSTestServer(t)

	// Create a lock
	lockBody := `{"path": "models/model.bin"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/myrepo.git/locks",
		bytes.NewBufferString(lockBody))
	req.Header.Set("Accept", metaMediaType)
	req.Header.Set("Content-Type", metaMediaType)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /myrepo.git/locks failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("Expected 201 Created, got %d", resp.StatusCode)
	}

	var lockResp struct {
		Lock *struct {
			ID   string `json:"id"`
			Path string `json:"path"`
		} `json:"lock"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&lockResp); err != nil {
		t.Fatalf("Failed to decode lock response: %v", err)
	}
	if lockResp.Lock == nil {
		t.Fatal("Expected lock in response, got nil")
	}
	if lockResp.Lock.Path != "models/model.bin" {
		t.Errorf("Expected path 'models/model.bin', got %q", lockResp.Lock.Path)
	}

	// List locks
	listReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/myrepo.git/locks", nil)
	listReq.Header.Set("Accept", metaMediaType)

	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatalf("GET /myrepo.git/locks failed: %v", err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 OK, got %d", listResp.StatusCode)
	}

	var lockList struct {
		Locks []struct {
			ID   string `json:"id"`
			Path string `json:"path"`
		} `json:"locks"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&lockList); err != nil {
		t.Fatalf("Failed to decode lock list: %v", err)
	}
	if len(lockList.Locks) != 1 {
		t.Errorf("Expected 1 lock, got %d", len(lockList.Locks))
	}
}
