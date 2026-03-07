package service

import (
	"context"
	"fmt"
	"pinn/internal/domain"
)

type ModelRepository interface {
	GetModelByID(ctx context.Context, modelID string) (*domain.Model, error)
	CreateModel(ctx context.Context, modelID string, containerImage string) (*domain.Model, error)
	DeleteModel(ctx context.Context, modelID string) error
	ListModels(ctx context.Context) ([]domain.Model, error)
	UpdateModel(ctx context.Context, modelID string, newContainerImage string) error
}

type ModelService struct {
	repository ModelRepository
}

func NewModelService(repository ModelRepository) *ModelService {
	return &ModelService{
		repository: repository,
	}
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

func (s *ModelService) CreateModel(ctx context.Context, modelID string, containerImage string) (*domain.Model, error) {
	model, err := s.repository.CreateModel(ctx, modelID, containerImage)
	if err != nil {
		return nil, fmt.Errorf("creating model: %w", err)
	}

	return model, nil
}

func (s *ModelService) DeleteModel(ctx context.Context, modelID string) error {
	if err := s.repository.DeleteModel(ctx, modelID); err != nil {
		return fmt.Errorf("deleting model: %w", err)
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
