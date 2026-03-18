package service

import (
	"context"
	"fmt"
	"io"
	"pinn/internal/domain"
	"strings"
)

type ModelRepository interface {
	GetModelByID(context.Context, string) (*domain.Model, error)
	CreateModel(ctx context.Context, modelID string, containerImage string) (*domain.Model, error)
	DeleteModel(context.Context, string) error
	ListModels(context.Context) ([]domain.Model, error)
	UpdateModel(ctx context.Context, modelID string, newContainerImage string) error
	Exists(ctx context.Context, id string) (bool, error)
}

type ModelManager interface {
	BuildImage(ctx context.Context, archive io.Reader, tag string, logWriter io.Writer) error
	RemoveImage(ctx context.Context, tag string) error
}

type ModelService struct {
	repository ModelRepository
	manager    ModelManager
}

func NewModelService(repository ModelRepository, manager ModelManager) *ModelService {
	return &ModelService{
		repository: repository,
		manager:    manager,
	}
}

func (s *ModelService) BuildModel(ctx context.Context, modelID string, archive io.Reader, logWriter io.Writer) error {
	tag := fmt.Sprintf("pinn-model-%s:latest", modelID)

	if err := s.manager.BuildImage(ctx, archive, tag, logWriter); err != nil {
		return fmt.Errorf("building docker image: %w", err)
	}

	_, err := s.repository.CreateModel(ctx, modelID, tag)
	if err != nil {
		return fmt.Errorf("saving model to repository: %w", err)
	}

	return nil
}

func (s *ModelService) GetImageByID(ctx context.Context, modelID string) (string, error) {
	model, err := s.repository.GetModelByID(ctx, modelID)
	if err != nil {
		return "", fmt.Errorf("getting model from repo: %w", err)
	}
	if model == nil {
		return "", nil
	}

	return model.ContainerImage, nil
}

func (s *ModelService) Exists(ctx context.Context, modelID string) (bool, error) {
	exists, err := s.repository.Exists(ctx, modelID)
	if err != nil {
		return false, fmt.Errorf("checking model exists: %w", err)
	}

	return exists, nil
}

func (s *ModelService) CreateModel(ctx context.Context, modelID string, containerImage string) (*domain.Model, error) {
	model, err := s.repository.CreateModel(ctx, modelID, containerImage)
	if err != nil {
		return nil, fmt.Errorf("creating model: %w", err)
	}

	return model, nil
}

func (s *ModelService) DeleteModel(ctx context.Context, modelID string) error {
	model, err := s.repository.GetModelByID(ctx, modelID)
	if err != nil {
		return fmt.Errorf("getting model info: %w", err)
	}

	if err := s.repository.DeleteModel(ctx, modelID); err != nil {
		return fmt.Errorf("deleting model from repo: %w", err)
	}

	if model != nil && strings.HasPrefix(model.ContainerImage, "pinn-model-") {
		go func() {
			if err := s.manager.RemoveImage(context.Background(), model.ContainerImage); err != nil {
				fmt.Printf("failed to remove image %s: %v\n", model.ContainerImage, err)
			}
		}()
	}

	return nil
}

func (s *ModelService) DeleteImageByModelId(ctx context.Context, modelID string) error {
	model, err := s.repository.GetModelByID(ctx, modelID)
	if err != nil {
		return fmt.Errorf("getting model info: %w", err)
	}

	if err := s.manager.RemoveImage(ctx, model.ContainerImage); err != nil {
		return fmt.Errorf("removing image: %w", err)
	}

	return nil
}

func (s *ModelService) ListModels(ctx context.Context) ([]domain.Model, error) {
	models, err := s.repository.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting models list: %w", err)
	}

	return models, nil
}

func (s *ModelService) UpdateModel(ctx context.Context, modelID string, newContainerImage string) error {
	if err := s.repository.UpdateModel(ctx, modelID, newContainerImage); err != nil {
		return fmt.Errorf("updating model: %w", err)
	}

	return nil
}
