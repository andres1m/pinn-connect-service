package repository

import (
	"context"
	"fmt"
	"pinn/internal/db"
	"pinn/internal/domain"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ModelRepository struct {
	queries *db.Queries
}

func NewModelRepository(pool *pgxpool.Pool) *ModelRepository {
	return &ModelRepository{queries: db.New(pool)}
}

func (r *ModelRepository) GetModelByID(ctx context.Context, modelID string) (*domain.Model, error) {
	dbmodel, err := r.queries.GetModelByID(ctx, modelID)
	if err != nil {
		return nil, fmt.Errorf("getting model by id: %w", err)
	}

	return dbModelToDomainModel(&dbmodel), nil
}

func (r *ModelRepository) ListModels(ctx context.Context) ([]domain.Model, error) {
	models, err := r.queries.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting list of models: %w", err)
	}

	result := []domain.Model{}
	for _, model := range models {
		result = append(result, *dbModelToDomainModel(&model))
	}

	return result, nil
}

func (r *ModelRepository) UpdateModel(ctx context.Context, modelID string, newContainerImage string) error {
	err := r.queries.UpdateModel(ctx, db.UpdateModelParams{
		ID:             modelID,
		ContainerImage: newContainerImage,
	})
	if err != nil {
		return fmt.Errorf("updating model: %w", err)
	}

	return nil
}

func (r *ModelRepository) CreateModel(ctx context.Context, modelID string, containerImage string) (*domain.Model, error) {
	model, err := r.queries.CreateModel(ctx, db.CreateModelParams{
		ID:             modelID,
		ContainerImage: containerImage,
	})

	if err != nil {
		return nil, fmt.Errorf("creating model: %w", err)
	}

	return dbModelToDomainModel(&model), nil
}

func (r *ModelRepository) DeleteModel(ctx context.Context, modelID string) error {
	if err := r.queries.DeleteModel(ctx, modelID); err != nil {
		return fmt.Errorf("deleting model: %w", err)
	}
	return nil
}

func dbModelToDomainModel(dbm *db.Model) *domain.Model {
	return &domain.Model{
		ID:             dbm.ID,
		ContainerImage: dbm.ContainerImage,
		CreatedAt:      dbm.CreatedAt.Time,
		UpdatedAt:      dbm.UpdatedAt.Time,
	}
}
