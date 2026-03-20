package service

import (
	"context"
	"errors"
	"testing"
)

// --- MOCKS ---

type mockPinger struct {
	checkStatusFunc func(context.Context) error
}

func (m *mockPinger) CheckStatus(ctx context.Context) error {
	if m.checkStatusFunc != nil {
		return m.checkStatusFunc(ctx)
	}
	return nil
}

// --- TESTS ---

func TestNewHealthService(t *testing.T) {
	container := &mockPinger{}
	storage := &mockPinger{}
	database := &mockPinger{}

	svc := NewHealthService(container, storage, database)
	if svc == nil {
		t.Fatal("NewHealthService returned nil")
	}
	if svc.containerSystemPinger != container || svc.storagePinger != storage || svc.databasePinger != database {
		t.Error("NewHealthService did not initialize fields correctly")
	}
}

func TestHealthService_CheckStatus(t *testing.T) {
	errTest := errors.New("ping failed")

	tests := []struct {
		name           string
		containerErr   error
		storageErr     error
		databaseErr    error
		wantErr        error
	}{
		{
			name:    "Success",
			wantErr: nil,
		},
		{
			name:         "Container System Failure",
			containerErr: errTest,
			wantErr:      errTest,
		},
		{
			name:       "Storage Failure",
			storageErr: errTest,
			wantErr:    errTest,
		},
		{
			name:        "Database Failure",
			databaseErr: errTest,
			wantErr:     errTest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			container := &mockPinger{checkStatusFunc: func(ctx context.Context) error { return tt.containerErr }}
			storage := &mockPinger{checkStatusFunc: func(ctx context.Context) error { return tt.storageErr }}
			database := &mockPinger{checkStatusFunc: func(ctx context.Context) error { return tt.databaseErr }}

			svc := NewHealthService(container, storage, database)
			err := svc.CheckStatus(context.Background())

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("CheckStatus() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
