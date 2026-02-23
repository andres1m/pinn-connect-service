package storage

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/url"
	"path/filepath"
	"pinn/internal/config"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type MinIOStorage struct {
	client *minio.Client
	bucket string
}

func NewMinIOStorage(config *config.Config) (*MinIOStorage, error) {
	client, err := minio.New(config.MinIOEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(config.MinIOAccesKey, config.MinIOSecretKey, ""),
		Secure: config.MinIOSSLUse,
	})
	if err != nil {
		return nil, fmt.Errorf("creating minio client: %w", err)
	}

	exists, err := client.BucketExists(context.Background(), config.MinIOBucket)
	if err != nil {
		return nil, fmt.Errorf("checking bucket exists: %w", err)
	}

	if !exists {
		err := client.MakeBucket(context.Background(), config.MinIOBucket, minio.MakeBucketOptions{})
		if err != nil {
			return nil, fmt.Errorf("creating bucket: %w", err)
		}
	}

	return &MinIOStorage{
		client: client,
		bucket: config.MinIOBucket,
	}, nil
}

func (m *MinIOStorage) Upload(ctx context.Context, objectKey string, r io.Reader, size int64) (string, error) {
	contentType := mime.TypeByExtension(filepath.Ext(objectKey))
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	_, err := m.client.PutObject(ctx, m.bucket, objectKey, r, size, minio.PutObjectOptions{
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

	presignedUrl, err := m.client.PresignedGetObject(ctx, m.bucket, objectKey, expiry, reqParams)
	if err != nil {
		return "", fmt.Errorf("generating minio download url: %w", err)
	}

	return presignedUrl.String(), nil
}
