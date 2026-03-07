package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"pinn/internal/config"
	"pinn/internal/domain"
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
	WaitContainer(context.Context, string) (int64, error)
	InspectContainer(context.Context, string) (*domain.ContainerStateResponse, error)
}

type ArtifactStorage interface {
	Upload(ctx context.Context, objectKey string, r io.Reader, size int64) (string, error)
	GetDownloadURL(ctx context.Context, objectKey string) (string, error)
}

type TaskRepository interface {
	Create(context.Context, *domain.Task) error
	GetTaskById(context.Context, uuid.UUID) (*domain.Task, error)
	FindCachedTask(context.Context, string) (string, error)
	Mark(context.Context, *domain.Task, domain.TaskStatus) error
	GetRunningTasksContainers(context.Context) ([]domain.RunningTasksContainer, error)
	GetNextQueuedTask(context.Context) (*domain.Task, error)
	GetScheduledTasks(context.Context, time.Time) ([]domain.Task, error)
}

type Workspace interface {
	Prepare(uuid.UUID) error
	ResultDir(uuid.UUID) string
	Cleanup(uuid.UUID) error
	SaveInput(taskID uuid.UUID, filename string, r io.Reader) error
}

type TaskService struct {
	manager      ContainerManager
	config       *config.Config
	storage      ArtifactStorage
	repository   TaskRepository
	workspace    Workspace
	modelService *ModelService
}

func NewTaskService(
	manager ContainerManager,
	storage ArtifactStorage,
	config *config.Config,
	repository TaskRepository,
	workspace Workspace,
	modelService *ModelService) *TaskService {
	return &TaskService{
		manager:      manager,
		storage:      storage,
		config:       config,
		repository:   repository,
		workspace:    workspace,
		modelService: modelService,
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

func (s *TaskService) StartScheduler(ctx context.Context) {
	ticker := time.NewTicker(20 * time.Second)
	var mu sync.Mutex

	scheduled := make(map[uuid.UUID]struct{})

	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				tasks, err := s.repository.GetScheduledTasks(ctx, time.Now().Add(30*time.Second))
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
	}()
}

func (s *TaskService) GetTask(ctx context.Context, id uuid.UUID) (*domain.Task, error) {
	result, err := s.repository.GetTaskById(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("getting task from repo: %w", err)
	}

	return result, nil
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

func (s *TaskService) StartWorker(ctx context.Context, wg *sync.WaitGroup) {
	sem := make(chan struct{}, s.config.MaxWorkers)
	ticker := time.NewTicker(2 * time.Second)

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

func (s *TaskService) processQueue(ctx context.Context, sem chan struct{}, wg *sync.WaitGroup) {
	for {
		select {
		case sem <- struct{}{}:

		default:
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

			taskCtx := context.WithoutCancel(ctx)

			if err := s.processTask(taskCtx, task); err != nil {
				slog.Error("error while processing task", "id", task.ID, "error", err)
			}
		})
	}
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
			markCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			s.repository.Mark(markCtx, task, domain.TaskFailed)
		}
	}()

	// start container
	containerID, err := s.manager.StartContainer(ctx, &domain.ContainerConfig{
		Image:  task.ContainerImage,
		Mounts: createMounts(s.config.TmpDir, task.ID),
		Cmd:    task.ContainerCmd,
		Envs:   task.ContainerEnvs,
	})
	if err != nil {
		return fmt.Errorf("starting container: %w", err)
	}

	defer func() {
		removeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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
	exitCode, err := s.manager.WaitContainer(ctx, task.ContainerID)
	if err != nil {
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

	// upload result to storage

	resPath, err := s.uploadToStorage(ctx, task.ID)
	if err != nil {
		return fmt.Errorf("upload to storage: %w", err)
	}

	task.ResultPath = resPath

	// mark as completed
	err = s.repository.Mark(ctx, task, domain.TaskCompleted)
	if err != nil {
		return fmt.Errorf("marking task completed: %w", err)
	}

	fmt.Println(time.Since(start))

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

func (s *TaskService) uploadToStorage(ctx context.Context, taskID uuid.UUID) (string, error) {
	entries, err := os.ReadDir(s.workspace.ResultDir(taskID))
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

	resultFilePath := filepath.Join(s.workspace.ResultDir(taskID), resultFileName)

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
	_, err = s.storage.Upload(ctx, objectKey, file, stat.Size())
	if err != nil {
		return "", fmt.Errorf("saving to S3 storage: %w", err)
	}

	return objectKey, nil
}
