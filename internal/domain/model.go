package domain

import "time"

type Model struct {
	ID             string
	ContainerImage string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
