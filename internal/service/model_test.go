package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"pinn-connect-service/internal/domain"
	"strings"
	"testing"
	"time"
)

// ─────────────────────────────────────────────
// MOCKS
// ─────────────────────────────────────────────

type fakeModelRepo struct {
	ModelRepository
	getByIDFunc func(context.Context, string) (*domain.Model, error)
	createFunc  func(context.Context, string, string) (*domain.Model, error)
	deleteFunc  func(context.Context, string) error
	listFunc    func(context.Context) ([]domain.Model, error)
	updateFunc  func(context.Context, string, string) error
	existsFunc  func(context.Context, string) (bool, error)
}

func (r *fakeModelRepo) GetModelByID(ctx context.Context, id string) (*domain.Model, error) {
	if r.getByIDFunc != nil {
		return r.getByIDFunc(ctx, id)
	}
	return &domain.Model{ID: id, ContainerImage: "pinn-model-" + id + ":latest"}, nil
}
func (r *fakeModelRepo) CreateModel(ctx context.Context, modelID, containerImage string) (*domain.Model, error) {
	if r.createFunc != nil {
		return r.createFunc(ctx, modelID, containerImage)
	}
	return &domain.Model{ID: modelID, ContainerImage: containerImage}, nil
}
func (r *fakeModelRepo) DeleteModel(ctx context.Context, id string) error {
	if r.deleteFunc != nil {
		return r.deleteFunc(ctx, id)
	}
	return nil
}
func (r *fakeModelRepo) ListModels(ctx context.Context) ([]domain.Model, error) {
	if r.listFunc != nil {
		return r.listFunc(ctx)
	}
	return []domain.Model{{ID: "m1"}, {ID: "m2"}}, nil
}
func (r *fakeModelRepo) UpdateModel(ctx context.Context, modelID, newImage string) error {
	if r.updateFunc != nil {
		return r.updateFunc(ctx, modelID, newImage)
	}
	return nil
}
func (r *fakeModelRepo) Exists(ctx context.Context, id string) (bool, error) {
	if r.existsFunc != nil {
		return r.existsFunc(ctx, id)
	}
	return true, nil
}

// ─────────────────────────────────────────────

type fakeModelManager struct {
	ModelManager
	buildFunc  func(context.Context, io.Reader, string, io.Writer) error
	removeFunc func(context.Context, string) error
}

func (m *fakeModelManager) BuildImage(ctx context.Context, archive io.Reader, tag string, logWriter io.Writer) error {
	if m.buildFunc != nil {
		return m.buildFunc(ctx, archive, tag, logWriter)
	}
	return nil
}
func (m *fakeModelManager) RemoveImage(ctx context.Context, tag string) error {
	if m.removeFunc != nil {
		return m.removeFunc(ctx, tag)
	}
	return nil
}

// ─────────────────────────────────────────────
// HELPER
// ─────────────────────────────────────────────

func newModelSvc(repo *fakeModelRepo, mgr *fakeModelManager) *ModelService {
	if repo == nil {
		repo = &fakeModelRepo{}
	}
	if mgr == nil {
		mgr = &fakeModelManager{}
	}
	return NewModelService(repo, mgr)
}

func emptyArchive() io.Reader { return bytes.NewReader([]byte("archive-data")) }
func devNull() io.Writer      { return io.Discard }

// ─────────────────────────────────────────────
// BuildModel
// ─────────────────────────────────────────────

func TestBuildModel_Success(t *testing.T) {
	var capturedTag string
	mgr := &fakeModelManager{
		buildFunc: func(_ context.Context, _ io.Reader, tag string, _ io.Writer) error {
			capturedTag = tag
			return nil
		},
	}
	svc := newModelSvc(nil, mgr)

	err := svc.BuildModel(context.Background(), "my-model", emptyArchive(), devNull())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedTag != "pinn-model-my-model:latest" {
		t.Errorf("unexpected image tag: %q", capturedTag)
	}
}

func TestBuildModel_BuildImageError(t *testing.T) {
	mgr := &fakeModelManager{
		buildFunc: func(_ context.Context, _ io.Reader, _ string, _ io.Writer) error {
			return errors.New("docker build failed")
		},
	}
	svc := newModelSvc(nil, mgr)

	err := svc.BuildModel(context.Background(), "m1", emptyArchive(), devNull())
	if err == nil {
		t.Fatal("expected error from BuildImage, got nil")
	}
	if !strings.Contains(err.Error(), "building docker image") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestBuildModel_CreateModelError(t *testing.T) {
	repo := &fakeModelRepo{
		createFunc: func(_ context.Context, _, _ string) (*domain.Model, error) {
			return nil, errors.New("db insert failed")
		},
	}
	svc := newModelSvc(repo, nil)

	err := svc.BuildModel(context.Background(), "m1", emptyArchive(), devNull())
	if err == nil {
		t.Fatal("expected error from CreateModel, got nil")
	}
	if !strings.Contains(err.Error(), "saving model to repository") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestBuildModel_TagFormat(t *testing.T) {
	var builtTag string
	mgr := &fakeModelManager{
		buildFunc: func(_ context.Context, _ io.Reader, tag string, _ io.Writer) error {
			builtTag = tag
			return nil
		},
	}
	svc := newModelSvc(nil, mgr)

	_ = svc.BuildModel(context.Background(), "special-model-123", emptyArchive(), devNull())
	if builtTag != "pinn-model-special-model-123:latest" {
		t.Errorf("bad tag: %q", builtTag)
	}
}

// ─────────────────────────────────────────────
// RebuildModel
// ─────────────────────────────────────────────

func TestRebuildModel_Success(t *testing.T) {
	var removedTag, builtTag string
	repo := &fakeModelRepo{
		getByIDFunc: func(_ context.Context, id string) (*domain.Model, error) {
			return &domain.Model{ID: id, ContainerImage: "pinn-model-" + id + ":latest"}, nil
		},
	}
	mgr := &fakeModelManager{
		removeFunc: func(_ context.Context, tag string) error { removedTag = tag; return nil },
		buildFunc:  func(_ context.Context, _ io.Reader, tag string, _ io.Writer) error { builtTag = tag; return nil },
	}
	svc := newModelSvc(repo, mgr)

	err := svc.RebuildModel(context.Background(), "m1", emptyArchive(), devNull())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if removedTag != "pinn-model-m1:latest" {
		t.Errorf("wrong image removed: %q", removedTag)
	}
	if builtTag != "pinn-model-m1:latest" {
		t.Errorf("wrong image built: %q", builtTag)
	}
}

func TestRebuildModel_GetByIdError(t *testing.T) {
	repo := &fakeModelRepo{
		getByIDFunc: func(_ context.Context, _ string) (*domain.Model, error) {
			return nil, errors.New("db error")
		},
	}
	svc := newModelSvc(repo, nil)

	err := svc.RebuildModel(context.Background(), "m1", emptyArchive(), devNull())
	if err == nil {
		t.Fatal("expected error from GetModelByID, got nil")
	}
	if !strings.Contains(err.Error(), "getting model info") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRebuildModel_ModelNotFound(t *testing.T) {
	repo := &fakeModelRepo{
		getByIDFunc: func(_ context.Context, _ string) (*domain.Model, error) {
			return nil, nil // not found
		},
	}
	svc := newModelSvc(repo, nil)

	err := svc.RebuildModel(context.Background(), "ghost", emptyArchive(), devNull())
	if err == nil {
		t.Fatal("expected error for missing model, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRebuildModel_RemoveImageError_ContinuesAndLogs(t *testing.T) {
	// RemoveImage failure should only produce a warning, not abort rebuild
	var logBuf bytes.Buffer
	repo := &fakeModelRepo{
		getByIDFunc: func(_ context.Context, id string) (*domain.Model, error) {
			return &domain.Model{ID: id, ContainerImage: "pinn-model-" + id + ":latest"}, nil
		},
	}
	mgr := &fakeModelManager{
		removeFunc: func(_ context.Context, _ string) error { return errors.New("remove failed") },
		buildFunc:  func(_ context.Context, _ io.Reader, _ string, _ io.Writer) error { return nil },
	}
	svc := newModelSvc(repo, mgr)

	err := svc.RebuildModel(context.Background(), "m1", emptyArchive(), &logBuf)
	if err != nil {
		t.Fatalf("expected nil error (remove failure should be a warning), got %v", err)
	}
	if !strings.Contains(logBuf.String(), "Warning") {
		t.Errorf("expected warning in log, got: %q", logBuf.String())
	}
}

func TestRebuildModel_EmptyContainerImage_SkipsRemove(t *testing.T) {
	// When ContainerImage is empty, RemoveImage must NOT be called
	removeCalled := false
	repo := &fakeModelRepo{
		getByIDFunc: func(_ context.Context, id string) (*domain.Model, error) {
			return &domain.Model{ID: id, ContainerImage: ""}, nil
		},
	}
	mgr := &fakeModelManager{
		removeFunc: func(_ context.Context, _ string) error { removeCalled = true; return nil },
		buildFunc:  func(_ context.Context, _ io.Reader, _ string, _ io.Writer) error { return nil },
	}
	svc := newModelSvc(repo, mgr)

	if err := svc.RebuildModel(context.Background(), "m1", emptyArchive(), devNull()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if removeCalled {
		t.Error("RemoveImage should not be called when ContainerImage is empty")
	}
}

func TestRebuildModel_BuildImageError(t *testing.T) {
	mgr := &fakeModelManager{
		buildFunc: func(_ context.Context, _ io.Reader, _ string, _ io.Writer) error {
			return errors.New("build failure")
		},
	}
	svc := newModelSvc(nil, mgr)

	err := svc.RebuildModel(context.Background(), "m1", emptyArchive(), devNull())
	if err == nil {
		t.Fatal("expected error from BuildImage, got nil")
	}
	if !strings.Contains(err.Error(), "rebuilding docker image") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRebuildModel_UpdateModelError(t *testing.T) {
	repo := &fakeModelRepo{
		updateFunc: func(_ context.Context, _, _ string) error {
			return errors.New("update failed")
		},
	}
	svc := newModelSvc(repo, nil)

	err := svc.RebuildModel(context.Background(), "m1", emptyArchive(), devNull())
	if err == nil {
		t.Fatal("expected error from UpdateModel, got nil")
	}
	if !strings.Contains(err.Error(), "updating model in repository") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// ─────────────────────────────────────────────
// GetImageByID
// ─────────────────────────────────────────────

func TestGetImageByID_Success(t *testing.T) {
	svc := newModelSvc(nil, nil)

	img, err := svc.GetImageByID(context.Background(), "m1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if img != "pinn-model-m1:latest" {
		t.Errorf("unexpected image: %q", img)
	}
}

func TestGetImageByID_ModelNotFound(t *testing.T) {
	repo := &fakeModelRepo{
		getByIDFunc: func(_ context.Context, _ string) (*domain.Model, error) {
			return nil, nil
		},
	}
	svc := newModelSvc(repo, nil)

	img, err := svc.GetImageByID(context.Background(), "ghost")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if img != "" {
		t.Errorf("expected empty image for missing model, got %q", img)
	}
}

func TestGetImageByID_RepoError(t *testing.T) {
	repo := &fakeModelRepo{
		getByIDFunc: func(_ context.Context, _ string) (*domain.Model, error) {
			return nil, errors.New("db error")
		},
	}
	svc := newModelSvc(repo, nil)

	_, err := svc.GetImageByID(context.Background(), "m1")
	if err == nil {
		t.Fatal("expected error from GetModelByID, got nil")
	}
	if !strings.Contains(err.Error(), "getting model from repo") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// ─────────────────────────────────────────────
// Exists
// ─────────────────────────────────────────────

func TestExists_True(t *testing.T) {
	svc := newModelSvc(nil, nil)

	exists, err := svc.Exists(context.Background(), "m1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Error("expected model to exist")
	}
}

func TestExists_False(t *testing.T) {
	repo := &fakeModelRepo{
		existsFunc: func(_ context.Context, _ string) (bool, error) { return false, nil },
	}
	svc := newModelSvc(repo, nil)

	exists, err := svc.Exists(context.Background(), "ghost")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("expected model not to exist")
	}
}

func TestExists_Error(t *testing.T) {
	repo := &fakeModelRepo{
		existsFunc: func(_ context.Context, _ string) (bool, error) {
			return false, errors.New("db error")
		},
	}
	svc := newModelSvc(repo, nil)

	_, err := svc.Exists(context.Background(), "m1")
	if err == nil {
		t.Fatal("expected error from Exists, got nil")
	}
	if !strings.Contains(err.Error(), "checking model exists") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// ─────────────────────────────────────────────
// CreateModel
// ─────────────────────────────────────────────

func TestCreateModel_Success(t *testing.T) {
	svc := newModelSvc(nil, nil)

	model, err := svc.CreateModel(context.Background(), "m1", "custom-img:v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model == nil {
		t.Fatal("expected non-nil model")
	}
	if model.ContainerImage != "custom-img:v1" {
		t.Errorf("unexpected container image: %q", model.ContainerImage)
	}
}

func TestCreateModel_RepoError(t *testing.T) {
	repo := &fakeModelRepo{
		createFunc: func(_ context.Context, _, _ string) (*domain.Model, error) {
			return nil, errors.New("unique constraint violation")
		},
	}
	svc := newModelSvc(repo, nil)

	_, err := svc.CreateModel(context.Background(), "m1", "img:v1")
	if err == nil {
		t.Fatal("expected error from CreateModel, got nil")
	}
	if !strings.Contains(err.Error(), "creating model") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// ─────────────────────────────────────────────
// DeleteModel
// ─────────────────────────────────────────────

func TestDeleteModel_Success_WithPinnImage(t *testing.T) {
	removeSignal := make(chan string, 1)
	repo := &fakeModelRepo{
		getByIDFunc: func(_ context.Context, id string) (*domain.Model, error) {
			return &domain.Model{ID: id, ContainerImage: "pinn-model-" + id + ":latest"}, nil
		},
	}
	mgr := &fakeModelManager{
		removeFunc: func(_ context.Context, tag string) error {
			removeSignal <- tag
			return nil
		},
	}
	svc := newModelSvc(repo, mgr)

	err := svc.DeleteModel(context.Background(), "m1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// goroutine fires async — give it a moment
	select {
	case tag := <-removeSignal:
		if tag != "pinn-model-m1:latest" {
			t.Errorf("wrong tag removed: %q", tag)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("RemoveImage goroutine did not fire in time")
	}
}

func TestDeleteModel_Success_NonPinnImage_SkipsRemove(t *testing.T) {
	// Images not prefixed with "pinn-model-" must not trigger RemoveImage
	removeCalled := make(chan struct{}, 1)
	repo := &fakeModelRepo{
		getByIDFunc: func(_ context.Context, id string) (*domain.Model, error) {
			return &domain.Model{ID: id, ContainerImage: "external-img:latest"}, nil
		},
	}
	mgr := &fakeModelManager{
		removeFunc: func(_ context.Context, _ string) error {
			removeCalled <- struct{}{}
			return nil
		},
	}
	svc := newModelSvc(repo, mgr)

	if err := svc.DeleteModel(context.Background(), "m1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-removeCalled:
		t.Error("RemoveImage should NOT be called for non-pinn images")
	case <-time.After(100 * time.Millisecond):
		// expected: no call
	}
}

func TestDeleteModel_NilModel_SkipsRemove(t *testing.T) {
	// GetModelByID returning nil must not call RemoveImage
	removeCalled := false
	repo := &fakeModelRepo{
		getByIDFunc: func(_ context.Context, _ string) (*domain.Model, error) { return nil, nil },
	}
	mgr := &fakeModelManager{
		removeFunc: func(_ context.Context, _ string) error { removeCalled = true; return nil },
	}
	svc := newModelSvc(repo, mgr)

	if err := svc.DeleteModel(context.Background(), "ghost"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	if removeCalled {
		t.Error("RemoveImage should not be called when model is nil")
	}
}

func TestDeleteModel_GetByIdError(t *testing.T) {
	repo := &fakeModelRepo{
		getByIDFunc: func(_ context.Context, _ string) (*domain.Model, error) {
			return nil, errors.New("db error")
		},
	}
	svc := newModelSvc(repo, nil)

	err := svc.DeleteModel(context.Background(), "m1")
	if err == nil {
		t.Fatal("expected error from GetModelByID, got nil")
	}
	if !strings.Contains(err.Error(), "getting model info") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestDeleteModel_DeleteRepoError(t *testing.T) {
	repo := &fakeModelRepo{
		deleteFunc: func(_ context.Context, _ string) error {
			return errors.New("delete constraint error")
		},
	}
	svc := newModelSvc(repo, nil)

	err := svc.DeleteModel(context.Background(), "m1")
	if err == nil {
		t.Fatal("expected error from DeleteModel, got nil")
	}
	if !strings.Contains(err.Error(), "deleting model from repo") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestDeleteModel_RemoveImageError_IsIgnored(t *testing.T) {
	// RemoveImage runs in a goroutine — its error is printed, not returned
	repo := &fakeModelRepo{
		getByIDFunc: func(_ context.Context, id string) (*domain.Model, error) {
			return &domain.Model{ID: id, ContainerImage: "pinn-model-" + id + ":latest"}, nil
		},
	}
	mgr := &fakeModelManager{
		removeFunc: func(_ context.Context, _ string) error {
			return errors.New("rmi failed")
		},
	}
	svc := newModelSvc(repo, mgr)

	err := svc.DeleteModel(context.Background(), "m1")
	if err != nil {
		t.Fatalf("expected nil (background error must be ignored), got: %v", err)
	}
	// allow goroutine to run
	time.Sleep(50 * time.Millisecond)
}

// ─────────────────────────────────────────────
// DeleteImageByModelId
// ─────────────────────────────────────────────

func TestDeleteImageByModelId_Success(t *testing.T) {
	var removedTag string
	mgr := &fakeModelManager{
		removeFunc: func(_ context.Context, tag string) error { removedTag = tag; return nil },
	}
	svc := newModelSvc(nil, mgr)

	err := svc.DeleteImageByModelId(context.Background(), "m1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if removedTag != "pinn-model-m1:latest" {
		t.Errorf("unexpected tag removed: %q", removedTag)
	}
}

func TestDeleteImageByModelId_GetByIdError(t *testing.T) {
	repo := &fakeModelRepo{
		getByIDFunc: func(_ context.Context, _ string) (*domain.Model, error) {
			return nil, errors.New("db error")
		},
	}
	svc := newModelSvc(repo, nil)

	err := svc.DeleteImageByModelId(context.Background(), "m1")
	if err == nil {
		t.Fatal("expected error from GetModelByID, got nil")
	}
	if !strings.Contains(err.Error(), "getting model info") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestDeleteImageByModelId_RemoveImageError(t *testing.T) {
	mgr := &fakeModelManager{
		removeFunc: func(_ context.Context, _ string) error {
			return errors.New("rmi error")
		},
	}
	svc := newModelSvc(nil, mgr)

	err := svc.DeleteImageByModelId(context.Background(), "m1")
	if err == nil {
		t.Fatal("expected error from RemoveImage, got nil")
	}
	if !strings.Contains(err.Error(), "removing image") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// ─────────────────────────────────────────────
// ListModels
// ─────────────────────────────────────────────

func TestListModels_Success(t *testing.T) {
	svc := newModelSvc(nil, nil)

	models, err := svc.ListModels(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) == 0 {
		t.Error("expected at least one model")
	}
}

func TestListModels_Empty(t *testing.T) {
	repo := &fakeModelRepo{
		listFunc: func(_ context.Context) ([]domain.Model, error) { return []domain.Model{}, nil },
	}
	svc := newModelSvc(repo, nil)

	models, err := svc.ListModels(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("expected empty list, got %d models", len(models))
	}
}

func TestListModels_RepoError(t *testing.T) {
	repo := &fakeModelRepo{
		listFunc: func(_ context.Context) ([]domain.Model, error) {
			return nil, errors.New("db error")
		},
	}
	svc := newModelSvc(repo, nil)

	_, err := svc.ListModels(context.Background())
	if err == nil {
		t.Fatal("expected error from ListModels, got nil")
	}
	if !strings.Contains(err.Error(), "getting models list") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// ─────────────────────────────────────────────
// UpdateModel
// ─────────────────────────────────────────────

func TestUpdateModel_Success(t *testing.T) {
	var updatedID, updatedImage string
	repo := &fakeModelRepo{
		updateFunc: func(_ context.Context, id, img string) error {
			updatedID = id
			updatedImage = img
			return nil
		},
	}
	svc := newModelSvc(repo, nil)

	err := svc.UpdateModel(context.Background(), "m1", "new-img:v2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updatedID != "m1" || updatedImage != "new-img:v2" {
		t.Errorf("unexpected update call: id=%q img=%q", updatedID, updatedImage)
	}
}

func TestUpdateModel_RepoError(t *testing.T) {
	repo := &fakeModelRepo{
		updateFunc: func(_ context.Context, _, _ string) error {
			return errors.New("update constraint")
		},
	}
	svc := newModelSvc(repo, nil)

	err := svc.UpdateModel(context.Background(), "m1", "img:v2")
	if err == nil {
		t.Fatal("expected error from UpdateModel, got nil")
	}
	if !strings.Contains(err.Error(), "updating model") {
		t.Errorf("unexpected error message: %v", err)
	}
}
