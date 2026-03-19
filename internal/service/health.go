package service

import "context"

type ContainerSystemPinger interface {
	CheckStatus(context.Context) error
}

type StoragePinger interface {
	CheckStatus(context.Context) error
}

type DatabasePinger interface {
	CheckStatus(context.Context) error
}

type HealthService struct {
	containerSystemPinger ContainerSystemPinger
	storagePinger         StoragePinger
	databasePinger        DatabasePinger
}

func NewHealthService(containerSystemPinger ContainerSystemPinger, storagePinger StoragePinger, databasePinger DatabasePinger) *HealthService {
	return &HealthService{
		containerSystemPinger: containerSystemPinger,
		storagePinger:         storagePinger,
		databasePinger:        databasePinger,
	}
}

func (s *HealthService) CheckStatus(ctx context.Context) error {
	if err := s.containerSystemPinger.CheckStatus(ctx); err != nil {
		return err
	}

	if err := s.storagePinger.CheckStatus(ctx); err != nil {
		return err
	}

	if err := s.databasePinger.CheckStatus(ctx); err != nil {
		return err
	}

	return nil
}
