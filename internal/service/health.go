package service

import "context"

type ContainerSystemPinger interface {
	CheckStatus(ctx context.Context) error
}

type HealthService struct {
	containerSystemPinger ContainerSystemPinger
}

func NewHealthService(containerSystemPinger ContainerSystemPinger) *HealthService {
	return &HealthService{containerSystemPinger: containerSystemPinger}
}

func (s *HealthService) CheckStatus(ctx context.Context) error {
	if err := s.containerSystemPinger.CheckStatus(ctx); err != nil {
		return err
	}

	return nil
}
