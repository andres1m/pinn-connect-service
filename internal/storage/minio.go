package storage

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"pinn-connect-service/internal/config"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type MinIOStorage struct {
	Client         *minio.Client
	clientExternal *minio.Client
	bucket         string
}

func NewMinIOStorage(ctx context.Context, config *config.Config) (*MinIOStorage, error) {
	client, err := minio.New(config.MinIO.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(config.MinIO.AccessKey, config.MinIO.SecretKey, ""),
		Secure: config.MinIO.SSLUse,
	})
	if err != nil {
		return nil, fmt.Errorf("creating minio client: %w", err)
	}

	clientExternal, err := minio.New(config.MinIO.ExternalEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(config.MinIO.AccessKey, config.MinIO.SecretKey, ""),
		Secure: config.MinIO.SSLUse,
		Region: "us-east-1",
	})
	if err != nil {
		return nil, fmt.Errorf("creating minio client: %w", err)
	}

	exists, err := client.BucketExists(ctx, config.MinIO.Bucket)
	if err != nil {
		return nil, fmt.Errorf("checking bucket exists: %w", err)
	}

	if !exists {
		err := client.MakeBucket(ctx, config.MinIO.Bucket, minio.MakeBucketOptions{})
		if err != nil {
			return nil, fmt.Errorf("creating bucket: %w", err)
		}
	}

	return &MinIOStorage{
		Client:         client,
		bucket:         config.MinIO.Bucket,
		clientExternal: clientExternal,
	}, nil
}

func (m *MinIOStorage) DeleteArtifacts(ctx context.Context, taskID uuid.UUID) error {
	prefix := fmt.Sprintf("tasks/%s/", taskID)

	objectsCh := m.Client.ListObjects(ctx, m.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})
	errorCh := m.Client.RemoveObjects(ctx, m.bucket, objectsCh, minio.RemoveObjectsOptions{})

	for err := range errorCh {
		if err.Err != nil {
			return fmt.Errorf("failed to remove object %s: %w", err.ObjectName, err.Err)
		}
	}

	return nil
}

func (m *MinIOStorage) UploadToStorage(ctx context.Context, taskID uuid.UUID, resultDir string) (string, error) {
	entries, err := os.ReadDir(resultDir)
	if err != nil {
		return "", fmt.Errorf("reading result dir: %w", err)
	}

	var resultFileName string
	for _, entry := range entries {
		if !entry.IsDir() {
			resultFileName = entry.Name()
			break
		}
	}

	if resultFileName == "" {
		return "", fmt.Errorf("no result file found in directory")
	}

	resultFilePath := filepath.Join(resultDir, resultFileName)

	file, err := os.Open(resultFilePath)
	if err != nil {
		return "", fmt.Errorf("opening result file: %w", err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("getting file stat: %w", err)
	}

	objectKey := fmt.Sprintf("tasks/%s/%s", taskID, resultFileName)
	_, err = m.upload(ctx, objectKey, file, stat.Size())
	if err != nil {
		return "", fmt.Errorf("saving to S3 storage: %w", err)
	}

	return objectKey, nil
}

func (m *MinIOStorage) upload(ctx context.Context, objectKey string, r io.Reader, size int64) (string, error) {
	contentType := mime.TypeByExtension(filepath.Ext(objectKey))
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	_, err := m.Client.PutObject(ctx, m.bucket, objectKey, r, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return "", fmt.Errorf("uploading into minio: %w", err)
	}

	return objectKey, nil
}

func (m *MinIOStorage) GetDownloadURL(ctx context.Context, objectKey string) (string, error) {
	expiry := 10 * time.Minute

	reqParams := make(url.Values)

	presignedUrl, err := m.clientExternal.PresignedGetObject(ctx, m.bucket, objectKey, expiry, reqParams)
	if err != nil {
		return "", fmt.Errorf("generating minio download url: %w", err)
	}

	return presignedUrl.String(), nil
}

func (m *MinIOStorage) CheckStatus(ctx context.Context) error {
	_, err := m.Client.BucketExists(ctx, m.bucket)
	return err
}
