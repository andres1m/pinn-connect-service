package repository

import (
	"context"
	"errors"
	"pinn-connect-service/internal/db"
	"pinn-connect-service/internal/domain"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	pgxmock "github.com/pashagolub/pgxmock/v4"
)

// ─────────────────────────────────────────────
// HELPERS
// ─────────────────────────────────────────────

func newModelRepoMock(t *testing.T) (*ModelRepository, pgxmock.PgxPoolIface) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock.NewPool: %v", err)
	}
	return &ModelRepository{queries: db.New(mock)}, mock
}

var modelColumns = []string{"id", "container_image", "created_at", "updated_at"}

func modelRow(id string) []any {
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	return []any{id, "pinn-model-" + id + ":latest", now, now}
}

// ─────────────────────────────────────────────
// GetModelByID
// ─────────────────────────────────────────────

func TestModelRepository_GetModelByID_Success(t *testing.T) {
	repo, mock := newModelRepoMock(t)

	mock.ExpectQuery(`SELECT`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows(modelColumns).AddRow(modelRow("m1")...))

	model, err := repo.GetModelByID(context.Background(), "m1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model == nil || model.ID != "m1" {
		t.Errorf("expected model 'm1', got %v", model)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestModelRepository_GetModelByID_NotFound_ReturnsNil(t *testing.T) {
	repo, mock := newModelRepoMock(t)

	mock.ExpectQuery(`SELECT`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnError(pgx.ErrNoRows)

	model, err := repo.GetModelByID(context.Background(), "ghost")
	if err != nil {
		t.Fatalf("expected nil error for not-found, got %v", err)
	}
	if model != nil {
		t.Error("expected nil model for not-found row")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestModelRepository_GetModelByID_DBError(t *testing.T) {
	repo, mock := newModelRepoMock(t)

	mock.ExpectQuery(`SELECT`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnError(errors.New("connection timeout"))

	if _, err := repo.GetModelByID(context.Background(), "m1"); err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestModelRepository_GetModelByID_FieldMapping(t *testing.T) {
	repo, mock := newModelRepoMock(t)
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}

	mock.ExpectQuery(`SELECT`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows(modelColumns).
			AddRow("m42", "custom-img:v2", now, now))

	model, err := repo.GetModelByID(context.Background(), "m42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model.ContainerImage != "custom-img:v2" {
		t.Errorf("expected 'custom-img:v2', got %q", model.ContainerImage)
	}
	if model.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be populated")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ─────────────────────────────────────────────
// ListModels
// ─────────────────────────────────────────────

func TestModelRepository_ListModels_Success(t *testing.T) {
	repo, mock := newModelRepoMock(t)

	// ListModels has no parameters
	mock.ExpectQuery(`SELECT`).
		WillReturnRows(pgxmock.NewRows(modelColumns).
			AddRow(modelRow("m1")...).
			AddRow(modelRow("m2")...))

	models, err := repo.ListModels(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 2 {
		t.Errorf("expected 2 models, got %d", len(models))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestModelRepository_ListModels_Empty(t *testing.T) {
	repo, mock := newModelRepoMock(t)

	mock.ExpectQuery(`SELECT`).
		WillReturnRows(pgxmock.NewRows(modelColumns))

	models, err := repo.ListModels(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// repository initialises result as []domain.Model{} so slice must be non-nil
	if models == nil {
		t.Error("expected non-nil empty slice")
	}
	if len(models) != 0 {
		t.Errorf("expected 0 models, got %d", len(models))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestModelRepository_ListModels_DBError(t *testing.T) {
	repo, mock := newModelRepoMock(t)

	mock.ExpectQuery(`SELECT`).WillReturnError(errors.New("db error"))

	if _, err := repo.ListModels(context.Background()); err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestModelRepository_ListModels_OrderPreserved(t *testing.T) {
	repo, mock := newModelRepoMock(t)

	mock.ExpectQuery(`SELECT`).
		WillReturnRows(pgxmock.NewRows(modelColumns).
			AddRow(modelRow("alpha")...).
			AddRow(modelRow("beta")...).
			AddRow(modelRow("gamma")...))

	models, err := repo.ListModels(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if models[0].ID != "alpha" || models[2].ID != "gamma" {
		t.Error("model ordering not preserved")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ─────────────────────────────────────────────
// UpdateModel
// ─────────────────────────────────────────────

func TestModelRepository_UpdateModel_Success(t *testing.T) {
	repo, mock := newModelRepoMock(t)

	// UpdateModel(containerImage, id) = 2 args, no RETURNING → Exec
	mock.ExpectExec(`UPDATE`).
		WithArgs(anyArgs(2)...).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	if err := repo.UpdateModel(context.Background(), "m1", "new-img:v2"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestModelRepository_UpdateModel_DBError(t *testing.T) {
	repo, mock := newModelRepoMock(t)

	mock.ExpectExec(`UPDATE`).
		WithArgs(anyArgs(2)...).
		WillReturnError(errors.New("no rows affected"))

	if err := repo.UpdateModel(context.Background(), "m1", "img:v2"); err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ─────────────────────────────────────────────
// Exists
// ─────────────────────────────────────────────

func TestModelRepository_Exists_True(t *testing.T) {
	repo, mock := newModelRepoMock(t)

	mock.ExpectQuery(`SELECT`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))

	exists, err := repo.Exists(context.Background(), "m1")
	if err != nil || !exists {
		t.Fatalf("expected true/nil, got %v/%v", exists, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestModelRepository_Exists_False(t *testing.T) {
	repo, mock := newModelRepoMock(t)

	mock.ExpectQuery(`SELECT`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))

	exists, err := repo.Exists(context.Background(), "ghost")
	if err != nil || exists {
		t.Fatalf("expected false/nil, got %v/%v", exists, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestModelRepository_Exists_DBError(t *testing.T) {
	repo, mock := newModelRepoMock(t)

	mock.ExpectQuery(`SELECT`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnError(errors.New("db error"))

	if _, err := repo.Exists(context.Background(), "m1"); err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ─────────────────────────────────────────────
// CreateModel
// ─────────────────────────────────────────────

func TestModelRepository_CreateModel_Success(t *testing.T) {
	repo, mock := newModelRepoMock(t)

	// CreateModel(id, containerImage) = 2 args, RETURNING → Query
	mock.ExpectQuery(`INSERT INTO`).
		WithArgs(anyArgs(2)...).
		WillReturnRows(pgxmock.NewRows(modelColumns).AddRow(modelRow("new-model")...))

	model, err := repo.CreateModel(context.Background(), "new-model", "img:v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model == nil || model.ID != "new-model" {
		t.Errorf("expected model 'new-model', got %v", model)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestModelRepository_CreateModel_FieldMapping(t *testing.T) {
	repo, mock := newModelRepoMock(t)
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}

	mock.ExpectQuery(`INSERT INTO`).
		WithArgs(anyArgs(2)...).
		WillReturnRows(pgxmock.NewRows(modelColumns).
			AddRow("m1", "img:v1", now, now))

	model, err := repo.CreateModel(context.Background(), "m1", "img:v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model.ContainerImage != "img:v1" {
		t.Errorf("expected 'img:v1', got %q", model.ContainerImage)
	}
	if model.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be populated")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestModelRepository_CreateModel_DBError(t *testing.T) {
	repo, mock := newModelRepoMock(t)

	mock.ExpectQuery(`INSERT INTO`).
		WithArgs(anyArgs(2)...).
		WillReturnError(errors.New("unique constraint violation"))

	if _, err := repo.CreateModel(context.Background(), "dup", "img:v1"); err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ─────────────────────────────────────────────
// DeleteModel
// ─────────────────────────────────────────────

func TestModelRepository_DeleteModel_Success(t *testing.T) {
	repo, mock := newModelRepoMock(t)

	// DeleteModel(id) = 1 arg, no RETURNING → Exec
	mock.ExpectExec(`DELETE`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("DELETE", 1))

	if err := repo.DeleteModel(context.Background(), "m1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestModelRepository_DeleteModel_DBError(t *testing.T) {
	repo, mock := newModelRepoMock(t)

	mock.ExpectExec(`DELETE`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnError(errors.New("foreign key violation"))

	if err := repo.DeleteModel(context.Background(), "m1"); err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ─────────────────────────────────────────────
// dbModelToDomainModel — pure unit, no DB
// ─────────────────────────────────────────────

func TestDbModelToDomainModel_FullMapping(t *testing.T) {
	now := time.Now().Truncate(time.Microsecond)
	src := db.Model{
		ID:             "model-xyz",
		ContainerImage: "registry.io/my-img:v99",
		CreatedAt:      pgtype.Timestamptz{Time: now, Valid: true},
		UpdatedAt:      pgtype.Timestamptz{Time: now, Valid: true},
	}

	got := dbModelToDomainModel(&src)

	if got.ID != "model-xyz" {
		t.Errorf("ID mismatch: %q", got.ID)
	}
	if got.ContainerImage != "registry.io/my-img:v99" {
		t.Errorf("ContainerImage mismatch: %q", got.ContainerImage)
	}
	if !got.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt mismatch: %v vs %v", got.CreatedAt, now)
	}
	if !got.UpdatedAt.Equal(now) {
		t.Errorf("UpdatedAt mismatch: %v vs %v", got.UpdatedAt, now)
	}
}

func TestDbModelToDomainModel_EmptyImage(t *testing.T) {
	got := dbModelToDomainModel(&db.Model{ID: "m1", ContainerImage: ""})
	if got.ContainerImage != "" {
		t.Errorf("expected empty ContainerImage, got %q", got.ContainerImage)
	}
}

func TestDbModelToDomainModel_ReturnType(t *testing.T) {
	got := dbModelToDomainModel(&db.Model{ID: "x"})
	if _, ok := interface{}(got).(*domain.Model); !ok {
		t.Error("expected *domain.Model return type")
	}
}
