package storage

// Strategy: run a fake S3 server with gofakes3 (implements the S3 API that
// MinIO client uses). Tests are in the same package so unexported fields and
// methods (upload, clientExternal) are accessible directly.
//
// Dependency (add once to go.mod):
//   go get github.com/johannesboyne/gofakes3@latest

import (
	"bytes"
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// ─────────────────────────────────────────────
// HELPERS
// ─────────────────────────────────────────────

const testBucket = "pinn-test-bucket"

// newFakeMinIOClient spins up an in-memory fake S3 server and returns a MinIO
// client pointed at it.  The server is shut down via t.Cleanup.
func newFakeMinIOClient(t *testing.T) *minio.Client {
	t.Helper()
	backend := s3mem.New()
	faker := gofakes3.New(backend)
	srv := httptest.NewServer(faker.Server())
	t.Cleanup(srv.Close)

	endpoint := strings.TrimPrefix(srv.URL, "http://")
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4("testaccess", "testsecret", ""),
		Secure: false,
	})
	if err != nil {
		t.Fatalf("newFakeMinIOClient: %v", err)
	}
	return client
}

// newTestStorage returns a fully wired MinIOStorage backed by the fake server.
// Both Client and clientExternal point to the same fake server (presigned URLs
// include the fake host, which is fine for unit testing).
func newTestStorage(t *testing.T) *MinIOStorage {
	t.Helper()
	client := newFakeMinIOClient(t)

	if err := client.MakeBucket(context.Background(), testBucket, minio.MakeBucketOptions{}); err != nil {
		t.Fatalf("newTestStorage: creating bucket: %v", err)
	}

	return &MinIOStorage{
		Client:         client,
		clientExternal: client,
		bucket:         testBucket,
	}
}

// tempDirWithFile creates a temp directory with one file and returns the dir path.
func tempDirWithFile(t *testing.T, filename, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644); err != nil {
		t.Fatalf("tempDirWithFile: %v", err)
	}
	return dir
}

// putObject is a helper that uploads an object directly through the client,
// bypassing storage methods, to set up test state.
func putObject(t *testing.T, s *MinIOStorage, key, content string) {
	t.Helper()
	data := []byte(content)
	_, err := s.Client.PutObject(
		context.Background(), s.bucket, key,
		bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: "application/octet-stream"},
	)
	if err != nil {
		t.Fatalf("putObject: %v", err)
	}
}

// objectExists returns true when the object is found in the bucket.
func objectExists(s *MinIOStorage, key string) bool {
	_, err := s.Client.StatObject(context.Background(), s.bucket, key, minio.StatObjectOptions{})
	return err == nil
}

// ─────────────────────────────────────────────
// CheckStatus
// ─────────────────────────────────────────────

func TestMinIOStorage_CheckStatus_Success(t *testing.T) {
	s := newTestStorage(t)
	if err := s.CheckStatus(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// A client pointing at an unreachable address must produce an error.
func TestMinIOStorage_CheckStatus_Unreachable(t *testing.T) {
	client, err := minio.New("127.0.0.1:1", &minio.Options{
		Creds:  credentials.NewStaticV4("k", "s", ""),
		Secure: false,
	})
	if err != nil {
		t.Fatalf("creating client: %v", err)
	}
	s := &MinIOStorage{Client: client, bucket: testBucket}
	if err := s.CheckStatus(context.Background()); err == nil {
		t.Fatal("expected error from unreachable server, got nil")
	}
}

// ─────────────────────────────────────────────
// upload (unexported)
// ─────────────────────────────────────────────

func TestMinIOStorage_Upload_Success_PlainText(t *testing.T) {
	s := newTestStorage(t)
	data := []byte("hello world")
	key := "tasks/abc/result.txt"

	got, err := s.upload(context.Background(), key, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != key {
		t.Errorf("expected returned key %q, got %q", key, got)
	}
	if !objectExists(s, key) {
		t.Errorf("expected object %q to exist in bucket after upload", key)
	}
}

func TestMinIOStorage_Upload_Success_JSON(t *testing.T) {
	s := newTestStorage(t)
	data := []byte(`{"status":"ok"}`)
	key := "tasks/abc/output.json"

	if _, err := s.upload(context.Background(), key, bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !objectExists(s, key) {
		t.Error("expected JSON object to exist")
	}
}

// An object key with no extension should use "application/octet-stream".
func TestMinIOStorage_Upload_DefaultContentType_NoExtension(t *testing.T) {
	s := newTestStorage(t)
	data := []byte("binary data")
	key := "tasks/abc/datafile" // no extension

	if _, err := s.upload(context.Background(), key, bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !objectExists(s, key) {
		t.Error("expected object with no extension to exist")
	}
}

// A client pointing at an unreachable address must propagate the error.
func TestMinIOStorage_Upload_ClientError(t *testing.T) {
	client, _ := minio.New("127.0.0.1:1", &minio.Options{
		Creds:  credentials.NewStaticV4("k", "s", ""),
		Secure: false,
	})
	s := &MinIOStorage{Client: client, bucket: testBucket}
	data := []byte("data")
	_, err := s.upload(context.Background(), "any/key.txt", bytes.NewReader(data), int64(len(data)))
	if err == nil {
		t.Fatal("expected error from unreachable server, got nil")
	}
}

// ─────────────────────────────────────────────
// UploadToStorage
// ─────────────────────────────────────────────

func TestMinIOStorage_UploadToStorage_Success(t *testing.T) {
	s := newTestStorage(t)
	id := uuid.New()
	dir := tempDirWithFile(t, "result.csv", "col1,col2\nval1,val2")

	key, err := s.UploadToStorage(context.Background(), id, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(key, id.String()) {
		t.Errorf("expected key to contain task ID %v, got %q", id, key)
	}
	if !strings.Contains(key, "result.csv") {
		t.Errorf("expected key to contain filename 'result.csv', got %q", key)
	}
	if !objectExists(s, key) {
		t.Errorf("expected object %q to exist in bucket", key)
	}
}

func TestMinIOStorage_UploadToStorage_DirNotFound(t *testing.T) {
	s := newTestStorage(t)
	_, err := s.UploadToStorage(context.Background(), uuid.New(), "/tmp/__nonexistent_dir_xyz_99999__")
	if err == nil {
		t.Fatal("expected error for non-existent directory, got nil")
	}
}

func TestMinIOStorage_UploadToStorage_EmptyDir(t *testing.T) {
	s := newTestStorage(t)
	dir := t.TempDir() // no files

	_, err := s.UploadToStorage(context.Background(), uuid.New(), dir)
	if err == nil {
		t.Fatal("expected error for empty directory, got nil")
	}
}

// When a directory has only subdirectories (no files), upload must fail.
func TestMinIOStorage_UploadToStorage_OnlySubdirectories(t *testing.T) {
	s := newTestStorage(t)
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatalf("creating subdir: %v", err)
	}

	_, err := s.UploadToStorage(context.Background(), uuid.New(), dir)
	if err == nil {
		t.Fatal("expected error when no files in directory, got nil")
	}
}

// The first non-directory entry is chosen; subdirectories before the file must
// be skipped.
func TestMinIOStorage_UploadToStorage_SkipsLeadingSubdirectory(t *testing.T) {
	s := newTestStorage(t)
	dir := t.TempDir()

	// os.ReadDir returns entries in lexicographic order.
	// "a-subdir" < "b-result.txt", so the subdir comes first.
	if err := os.Mkdir(filepath.Join(dir, "a-subdir"), 0o755); err != nil {
		t.Fatalf("creating subdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b-result.txt"), []byte("data"), 0o644); err != nil {
		t.Fatalf("creating file: %v", err)
	}

	id := uuid.New()
	key, err := s.UploadToStorage(context.Background(), id, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(key, "b-result.txt") {
		t.Errorf("expected key to contain 'b-result.txt', got %q", key)
	}
}

// The object key must follow the "tasks/<id>/<filename>" pattern.
func TestMinIOStorage_UploadToStorage_KeyFormat(t *testing.T) {
	s := newTestStorage(t)
	id := uuid.New()
	dir := tempDirWithFile(t, "model_output.zip", "zip-content")

	key, err := s.UploadToStorage(context.Background(), id, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := fmt.Sprintf("tasks/%s/model_output.zip", id)
	if key != expected {
		t.Errorf("expected key %q, got %q", expected, key)
	}
}

// ─────────────────────────────────────────────
// DeleteArtifacts
// ─────────────────────────────────────────────

func TestMinIOStorage_DeleteArtifacts_Success(t *testing.T) {
	s := newTestStorage(t)
	id := uuid.New()
	ctx := context.Background()

	keys := []string{
		fmt.Sprintf("tasks/%s/file1.txt", id),
		fmt.Sprintf("tasks/%s/file2.json", id),
	}
	for _, k := range keys {
		putObject(t, s, k, "artifact content")
	}

	if err := s.DeleteArtifacts(ctx, id); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, k := range keys {
		if objectExists(s, k) {
			t.Errorf("expected object %q to be deleted, but it still exists", k)
		}
	}
}

// Deleting with a prefix that matches no objects must succeed without error.
func TestMinIOStorage_DeleteArtifacts_NoMatchingObjects(t *testing.T) {
	s := newTestStorage(t)
	if err := s.DeleteArtifacts(context.Background(), uuid.New()); err != nil {
		t.Fatalf("unexpected error when no objects match prefix: %v", err)
	}
}

// Deleting artifacts must only remove objects under the task's prefix, not
// objects belonging to other tasks.
func TestMinIOStorage_DeleteArtifacts_IsolatedByPrefix(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	targetID := uuid.New()
	otherID := uuid.New()

	targetKey := fmt.Sprintf("tasks/%s/output.txt", targetID)
	otherKey := fmt.Sprintf("tasks/%s/output.txt", otherID)

	putObject(t, s, targetKey, "target")
	putObject(t, s, otherKey, "other")

	if err := s.DeleteArtifacts(ctx, targetID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if objectExists(s, targetKey) {
		t.Error("target object should have been deleted")
	}
	if !objectExists(s, otherKey) {
		t.Error("other task's object should NOT have been deleted")
	}
}

func TestMinIOStorage_DeleteArtifacts_MultipleFiles(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	id := uuid.New()

	// Upload several files
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("tasks/%s/file%d.dat", id, i)
		putObject(t, s, key, fmt.Sprintf("content%d", i))
	}

	if err := s.DeleteArtifacts(ctx, id); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("tasks/%s/file%d.dat", id, i)
		if objectExists(s, key) {
			t.Errorf("expected %q to be deleted", key)
		}
	}
}

// ─────────────────────────────────────────────
// GetDownloadURL
// ─────────────────────────────────────────────

func TestMinIOStorage_GetDownloadURL_Success(t *testing.T) {
	s := newTestStorage(t)
	id := uuid.New()
	objectKey := fmt.Sprintf("tasks/%s/result.zip", id)

	rawURL, err := s.GetDownloadURL(context.Background(), objectKey)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rawURL == "" {
		t.Fatal("expected non-empty presigned URL")
	}
	if !strings.Contains(rawURL, objectKey) {
		t.Errorf("expected presigned URL to contain object key %q, got %q", objectKey, rawURL)
	}
}

// The returned URL must be a syntactically valid absolute URL.
func TestMinIOStorage_GetDownloadURL_ReturnsValidURL(t *testing.T) {
	s := newTestStorage(t)
	rawURL, err := s.GetDownloadURL(context.Background(), "tasks/abc/file.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		t.Errorf("expected absolute HTTP(S) URL, got %q", rawURL)
	}
}

// Presigned URLs must contain an X-Amz-Signature query parameter (proof that
// signing took place).
func TestMinIOStorage_GetDownloadURL_ContainsSignature(t *testing.T) {
	s := newTestStorage(t)
	rawURL, err := s.GetDownloadURL(context.Background(), "tasks/abc/file.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(rawURL, "X-Amz-Signature") {
		t.Errorf("expected presigned URL to contain X-Amz-Signature, got %q", rawURL)
	}
}

// Different object keys must produce different presigned URLs.
func TestMinIOStorage_GetDownloadURL_DifferentKeysProduceDifferentURLs(t *testing.T) {
	s := newTestStorage(t)
	url1, err := s.GetDownloadURL(context.Background(), "tasks/aaa/file1.zip")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	url2, err := s.GetDownloadURL(context.Background(), "tasks/bbb/file2.zip")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url1 == url2 {
		t.Error("expected different URLs for different keys")
	}
}

// An empty bucket name should cause PresignedGetObject to return an error.
func TestMinIOStorage_GetDownloadURL_InvalidBucket_Error(t *testing.T) {
	client := newFakeMinIOClient(t)
	s := &MinIOStorage{
		clientExternal: client,
		bucket:         "", // invalid — empty bucket name
	}
	if _, err := s.GetDownloadURL(context.Background(), "tasks/abc/file.txt"); err == nil {
		t.Fatal("expected error for empty bucket name, got nil")
	}
}
