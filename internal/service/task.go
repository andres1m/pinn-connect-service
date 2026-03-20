package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"pinn-connect-service/internal/config"
	"pinn-connect-service/internal/domain"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/pkg/stdcopy"
	"github.com/google/uuid"
)

type ContainerManager interface {
	StartContainer(context.Context, *domain.ContainerConfig) (string, error)
	GetContainerLogs(ctx context.Context, containerID string, follow bool) (io.ReadCloser, error)
	RemoveContainer(context.Context, string) error
	StopContainer(ctx context.Context, id string, timeout time.Duration) error
	WaitContainer(context.Context, string) (int64, error)
	GetContainerState(context.Context, string) (*domain.ContainerState, error)
}

type ArtifactStorage interface {
	UploadToStorage(ctx context.Context, taskID uuid.UUID, resultDir string) (string, error)
	GetDownloadURL(ctx context.Context, objectKey string) (string, error)
	DeleteArtifacts(ctx context.Context, taskID uuid.UUID) error
}

type TaskRepository interface {
	Create(context.Context, *domain.Task) error
	GetTaskById(context.Context, uuid.UUID) (*domain.Task, error)
	FindCachedTask(context.Context, string) (string, error)
	Mark(context.Context, *domain.Task, domain.TaskStatus) error
	GetNextQueuedTask(context.Context) (*domain.Task, error)
	GetScheduledTasks(context.Context, time.Time) ([]domain.Task, error)
	GetTasksPaginated(context.Context, int32, int32) ([]domain.Task, error)
	GetTasksCount(context.Context) (int64, error)
	DeleteTask(context.Context, uuid.UUID) error
}

type Workspace interface {
	Prepare(uuid.UUID) error
	ResultDir(uuid.UUID) string
	Cleanup(uuid.UUID) error
	SaveInput(taskID uuid.UUID, filename string, r io.Reader) error
}

type TaskService struct {
	manager          ContainerManager
	config           *config.Config
	storage          ArtifactStorage
	repository       TaskRepository
	workspace        Workspace
	modelService     *ModelService
	recoverTaskQueue []*domain.Task
	recoverMu        sync.Mutex
}

func NewTaskService(
	manager ContainerManager,
	storage ArtifactStorage,
	config *config.Config,
	repository TaskRepository,
	workspace Workspace,
	modelService *ModelService) *TaskService {
	return &TaskService{
		manager:          manager,
		storage:          storage,
		config:           config,
		repository:       repository,
		workspace:        workspace,
		modelService:     modelService,
		recoverTaskQueue: make([]*domain.Task, 0),
		recoverMu:        sync.Mutex{},
	}
}

func (s *TaskService) SaveInput(taskID uuid.UUID, filename string, r io.Reader) ([]byte, error) {
	if err := s.workspace.Prepare(taskID); err != nil {
		return nil, fmt.Errorf("preparing task workspace: %w", err)
	}

	hasher := sha256.New()
	tee := io.TeeReader(r, hasher)

	if err := s.workspace.SaveInput(taskID, filename, tee); err != nil {
		return nil, fmt.Errorf("saving input file in task workspace: %w", err)
	}

	return hasher.Sum(nil), nil
}

func (s *TaskService) CreateTask(ctx context.Context, task *domain.Task, fileHash []byte) (err error) {
	defer func() {
		if err != nil {
			if cleanupErr := s.cleanupWorkspace(task.ID); cleanupErr != nil {
				slog.Error("failed to cleanup workspace", "task_id", task.ID, "error", cleanupErr)
			}
		}
	}()

	contImg, err := s.modelService.GetImageByID(ctx, task.ModelID)
	if err != nil {
		return fmt.Errorf("getting container image by model id: %w", err)
	}

	if contImg == "" {
		err = domain.ErrModelNotFound
		return err
	}

	task.ContainerImage = contImg

	var signature string
	signature, err = getFinalHash(task, fileHash)
	if err != nil {
		return fmt.Errorf("hashing task meta: %w", err)
	}

	slog.Info("DEBUG: Task Signature",
		"task_id", task.ID,
		"signature", signature,
		"file_hash", hex.EncodeToString(fileHash),
		"envs", task.ContainerEnvs,
		"cmd", task.ContainerCmd,
	)

	task.Signature = signature

	var resultPath string
	resultPath, err = s.findCachedTask(ctx, task.Signature)

	if err != nil {
		slog.Error("finding cached task", "error", err)
	} else if resultPath != "" {
		task.ResultPath = resultPath
		task.Status = domain.TaskCompleted

		if err = s.initTask(ctx, task); err != nil {
			return fmt.Errorf("saving task using task service: %w", err)
		}

		if err = s.cleanupWorkspace(task.ID); err != nil {
			return fmt.Errorf("cleaning workspace: %w", err)
		}

		return nil
	}

	task.Status = domain.TaskQueued
	if task.ScheduledAt != nil && task.ScheduledAt.After(time.Now()) {
		task.Status = domain.TaskScheduled
	}

	return s.initTask(ctx, task)
}

func (s *TaskService) StopTask(ctx context.Context, taskID uuid.UUID, timeout time.Duration) error {
	task, err := s.repository.GetTaskById(ctx, taskID)
	if err != nil {
		return fmt.Errorf("getting task by id: %w", err)
	}

	if task.ContainerID == "" {
		return fmt.Errorf("task container id is empty")
	}

	if err := s.repository.Mark(ctx, task, domain.TaskStopped); err != nil {
		return fmt.Errorf("marking task stopped: %w", err)
	}

	if err := s.manager.StopContainer(ctx, task.ContainerID, timeout); err != nil {
		return fmt.Errorf("stopping container: %w", err)
	}

	return nil
}

func (s *TaskService) StartScheduler(ctx context.Context, wg *sync.WaitGroup) {
	ticker := time.NewTicker(s.config.Scheduler.Interval)
	var mu sync.Mutex

	scheduled := make(map[uuid.UUID]struct{})

	wg.Go(func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				tasks, err := s.repository.GetScheduledTasks(ctx, time.Now().Add(s.config.Scheduler.TaskExpires))
				if err != nil {
					slog.Error("error getting scheduled task")
					continue
				}

				for _, task := range tasks {
					mu.Lock()

					if _, ok := scheduled[task.ID]; ok {
						mu.Unlock()
						continue
					}

					scheduled[task.ID] = struct{}{}
					mu.Unlock()

					delay := time.Until(*task.ScheduledAt)
					time.AfterFunc(delay, func() {
						s.repository.Mark(ctx, &task, domain.TaskQueued)
						mu.Lock()
						delete(scheduled, task.ID)
						mu.Unlock()
					})
				}
			}
		}
	})
}

func (s *TaskService) GetTask(ctx context.Context, id uuid.UUID) (*domain.Task, error) {
	result, err := s.repository.GetTaskById(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("getting task from repo: %w", err)
	}

	return result, nil
}

func (s *TaskService) ListTasks(ctx context.Context, page, pageSize int) ([]domain.Task, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}

	offset := (page - 1) * pageSize

	tasks, err := s.repository.GetTasksPaginated(ctx, int32(pageSize), int32(offset))
	if err != nil {
		return nil, 0, fmt.Errorf("getting paginated tasks from repo: %w", err)
	}

	total, err := s.repository.GetTasksCount(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("getting total tasks count: %w", err)
	}

	return tasks, total, nil
}

func (s *TaskService) DeleteTask(ctx context.Context, id uuid.UUID) error {
	if err := s.storage.DeleteArtifacts(ctx, id); err != nil {
		return fmt.Errorf("deleting artifacts: %w", err)
	}

	if err := s.workspace.Cleanup(id); err != nil {
		return fmt.Errorf("deleting workspace: %w", err)
	}

	return s.repository.DeleteTask(ctx, id)
}

func (s *TaskService) GetResultURL(ctx context.Context, id uuid.UUID) (string, error) {
	task, err := s.repository.GetTaskById(ctx, id)
	if err != nil {
		return "", fmt.Errorf("getting task from repo: %w", err)
	}

	if task.Status != domain.TaskCompleted {
		return "", nil
	}

	result, err := s.storage.GetDownloadURL(ctx, task.ResultPath)
	if err != nil {
		return "", fmt.Errorf("getting storage download link: %w", err)
	}

	return result, nil
}

func (s *TaskService) RecoverTask(task *domain.Task) {
	s.recoverMu.Lock()
	s.recoverTaskQueue = append(s.recoverTaskQueue, task)
	s.recoverMu.Unlock()
}

func (s *TaskService) StartWorker(ctx context.Context, wg *sync.WaitGroup) {
	sem := make(chan struct{}, s.config.Worker.MaxWorkers)
	ticker := time.NewTicker(s.config.Worker.Interval)

	wg.Go(func() {
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				slog.Info("worker stopped, waiting for active tasks to finish...")
				// filling the semaphore ensures that all goroutine tasks are completed
				for i := 0; i < cap(sem); i++ {
					sem <- struct{}{}
				}
				slog.Info("all active tasks finished")
				return

			case <-ticker.C:
				s.processQueue(ctx, sem, wg)
			}
		}
	})
}

func (s *TaskService) getNextRecoverTask() *domain.Task {
	s.recoverMu.Lock()
	defer s.recoverMu.Unlock()

	if len(s.recoverTaskQueue) == 0 {
		return nil
	}

	task := s.recoverTaskQueue[0]
	if len(s.recoverTaskQueue) > 0 {
		s.recoverTaskQueue = s.recoverTaskQueue[1:]
	}

	return task
}

func (s *TaskService) processQueue(ctx context.Context, sem chan struct{}, wg *sync.WaitGroup) {
	for {
		select {
		case sem <- struct{}{}:

		default:
			return
		}

		recTask := s.getNextRecoverTask()
		if recTask != nil {
			wg.Go(func() { s.waitAndSaveTask(ctx, recTask) })
			<-sem
			return
		}

		task, err := s.repository.GetNextQueuedTask(ctx)
		if err != nil {
			slog.Error("failed to get next task", "error", err)
			<-sem
			return
		}

		if task == nil {
			<-sem
			return // no queued tasks, waiting for next call
		}

		resPath, err := s.repository.FindCachedTask(ctx, task.Signature)
		if err != nil {
			slog.Error("while finding task in cache", "error", err)
		} else if resPath != "" {
			task.ResultPath = resPath
			s.mark(ctx, task, domain.TaskCompleted)

			<-sem
			return
		}

		wg.Go(func() {
			defer func() { <-sem }()

			taskCtx, cancel := context.WithTimeout(ctx, time.Duration(task.TimeoutSec)*time.Second)
			defer cancel()

			if err := s.processTask(taskCtx, task); err != nil {
				slog.Error("error while processing task", "id", task.ID, "error", err)
			}
		})
	}
}

// worker gouroutine should use it
func (s *TaskService) processTask(ctx context.Context, task *domain.Task) (err error) {
	start := time.Now()
	defer func() {
		if err := s.workspace.Cleanup(task.ID); err != nil {
			slog.Error("Error while cleanup workspace", "error", err)
		}
	}()

	// mark failed if err occurs while run
	defer func() {
		if err != nil {
			markCtx, cancel := context.WithTimeout(context.Background(), s.config.Worker.ProcessTaskCleanupTimeout)
			defer cancel()
			s.repository.Mark(markCtx, task, domain.TaskFailed)
		}
	}()

	dbtask, err := s.repository.GetTaskById(ctx, task.ID)
	if err != nil || dbtask.Status == domain.TaskStopped {
		return fmt.Errorf("checking is task stopped: %w", err)
	}

	// start container
	containerID, err := s.manager.StartContainer(ctx, &domain.ContainerConfig{
		Image:  task.ContainerImage,
		Mounts: createMounts(s.config.TmpDir, task.ID),
		Cmd:    task.ContainerCmd,
		Envs:   task.ContainerEnvs,
		TaskID: task.ID,
	})
	if err != nil {
		return fmt.Errorf("starting container: %w", err)
	}

	defer func() {
		removeCtx, cancel := context.WithTimeout(context.Background(), s.config.Worker.ProcessTaskCleanupTimeout)
		defer cancel()

		if err := s.manager.RemoveContainer(removeCtx, containerID); err != nil {
			slog.Error("Error while removing container", "error", err)
		}
	}()

	task.ContainerID = containerID

	// mark as running
	err = s.repository.Mark(ctx, task, domain.TaskRunning)
	if err != nil {
		return fmt.Errorf("marking task running: %w", err)
	}

	// wait for container end
	err = s.waitAndSaveTask(ctx, task)
	if err != nil {
		return fmt.Errorf("waiting and saving task: %w", err)
	}

	fmt.Println(time.Since(start))

	return nil
}

func (s *TaskService) waitAndSaveTask(ctx context.Context, task *domain.Task) error {
	exitCode, err := s.manager.WaitContainer(ctx, task.ContainerID)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			origErr := err
			stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			if err := s.manager.StopContainer(stopCtx, task.ContainerID, 2*time.Second); err != nil {
				slog.Error("failed to stop container during timeout cleanup", "error", err)
			}

			if err := s.repository.Mark(stopCtx, task, domain.TaskStopped); err != nil {
				slog.Error("failed to mark container stopped during timeout cleanup", "error", err)
			}

			err = nil
			return fmt.Errorf("task timed out: %w", origErr) //TODO maybe return nil instead
		}

		errorLog, logErr := s.getErrLogs(ctx, task.ContainerID)
		if logErr != nil {
			return fmt.Errorf("container failed: %w (also error while getting container error logs: %w)", err, logErr)
		}

		task.ErrorLog = errorLog

		return fmt.Errorf("container failed: %w, stderr logs: %s", err, errorLog)
	}

	// check container status
	var errorLog string
	if exitCode != 0 {
		errorLog, err = s.getErrLogs(ctx, task.ContainerID)
		if err != nil {
			return fmt.Errorf("getting container error logs: %w", err)
		}

		task.ErrorLog = errorLog

		return fmt.Errorf("container exited with not null value: %s", errorLog)
	}

	// if task was stopped -> stop worker
	dbtask, err := s.repository.GetTaskById(ctx, task.ID)
	if err != nil {
		return fmt.Errorf("getting task from repository: %w", err)
	}

	if dbtask.Status == domain.TaskStopped {
		return nil
	}

	// upload result to storage
	resPath, err := s.storage.UploadToStorage(ctx, task.ID, s.workspace.ResultDir(task.ID))
	if err != nil {
		return fmt.Errorf("upload to storage: %w", err)
	}

	task.ResultPath = resPath

	// mark as completed
	err = s.repository.Mark(ctx, task, domain.TaskCompleted)
	if err != nil {
		return fmt.Errorf("marking task completed: %w", err)
	}

	return nil
}

func getFinalHash(task *domain.Task, fileHash []byte) (string, error) {
	sort.Strings(task.ContainerEnvs)
	finalHasher := sha256.New()
	hashMeta := []byte(task.ModelID + "|" + strings.Join(task.ContainerEnvs, ",") + "|" + strings.Join(task.ContainerCmd, ",") + "|")

	if _, err := finalHasher.Write(hashMeta); err != nil {
		return "", fmt.Errorf("writing to hasher: %w", err)
	}
	if _, err := finalHasher.Write(fileHash); err != nil {
		return "", fmt.Errorf("writing to hasher: %w", err)
	}

	return hex.EncodeToString(finalHasher.Sum(nil)), nil
}

func (s *TaskService) initTask(ctx context.Context, task *domain.Task) error {
	if err := s.repository.Create(ctx, task); err != nil {
		return fmt.Errorf("creating task in repository: %w", err)
	}
	return nil
}

func (s *TaskService) mark(ctx context.Context, task *domain.Task, status domain.TaskStatus) error {
	if err := s.repository.Mark(ctx, task, status); err != nil {
		return fmt.Errorf("marking task: %w", err)
	}
	return nil
}

func (s *TaskService) findCachedTask(ctx context.Context, signature string) (string, error) {
	resPath, err := s.repository.FindCachedTask(ctx, signature)
	if err != nil {
		return "", fmt.Errorf("finding cached task: %w", err)
	}
	return resPath, nil
}

func (s *TaskService) cleanupWorkspace(taskID uuid.UUID) error {
	if err := s.workspace.Cleanup(taskID); err != nil {
		return fmt.Errorf("cleaning up task workspace: %w", err)
	}
	return nil
}

func createMounts(tmpdir string, taskID uuid.UUID) []domain.Mount {
	return []domain.Mount{
		{
			Source:   filepath.Join(tmpdir, taskID.String(), "input"),
			Target:   "/app/input",
			ReadOnly: true,
		},
		{
			Source:   filepath.Join(tmpdir, taskID.String(), "result"),
			Target:   "/app/result",
			ReadOnly: false,
		},
	}
}

func (s *TaskService) getErrLogs(ctx context.Context, containerID string) (string, error) {
	rc, err := s.manager.GetContainerLogs(ctx, containerID, false)
	if err != nil {
		return "", fmt.Errorf("fetching container err logs: %w", err)
	}
	defer rc.Close()

	var stdout, stderr bytes.Buffer

	_, err = stdcopy.StdCopy(&stdout, &stderr, rc)
	if err != nil {
		return "", fmt.Errorf("reading container err logs: %w", err)
	}

	return stderr.String(), nil
}
