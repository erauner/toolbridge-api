package tools

import (
	"fmt"
	"github.com/google/uuid"
)

type ListTasksParams struct {
	Cursor         *string `json:"cursor,omitempty"`
	Limit          *int    `json:"limit,omitempty"`
	IncludeDeleted bool    `json:"includeDeleted,omitempty"`
}

func (p *ListTasksParams) Validate() error {
	if p.Limit != nil && (*p.Limit < 1 || *p.Limit > 1000) {
		return fmt.Errorf("limit must be between 1 and 1000")
	}
	return nil
}

type GetTaskParams struct {
	UID            string `json:"uid"`
	IncludeDeleted bool   `json:"includeDeleted,omitempty"`
}

func (p *GetTaskParams) Validate() error {
	if _, err := uuid.Parse(p.UID); err != nil {
		return fmt.Errorf("invalid uid: %w", err)
	}
	return nil
}

func (p *GetTaskParams) ParseUID() (uuid.UUID, error) {
	return uuid.Parse(p.UID)
}

type CreateTaskParams struct {
	UID     *string        `json:"uid,omitempty"`
	Payload map[string]any `json:"payload"`
}

func (p *CreateTaskParams) Validate() error {
	if p.Payload == nil || len(p.Payload) == 0 {
		return fmt.Errorf("payload is required and cannot be empty")
	}
	if p.UID != nil {
		if _, err := uuid.Parse(*p.UID); err != nil {
			return fmt.Errorf("invalid uid: %w", err)
		}
	}
	return nil
}

func (p *CreateTaskParams) ParseUID() (*uuid.UUID, error) {
	if p.UID == nil {
		return nil, nil
	}
	uid, err := uuid.Parse(*p.UID)
	if err != nil {
		return nil, err
	}
	return &uid, nil
}

type UpdateTaskParams struct {
	UID     string         `json:"uid"`
	Payload map[string]any `json:"payload"`
	Version *int           `json:"version,omitempty"`
}

func (p *UpdateTaskParams) Validate() error {
	if _, err := uuid.Parse(p.UID); err != nil {
		return fmt.Errorf("invalid uid: %w", err)
	}
	if p.Payload == nil || len(p.Payload) == 0 {
		return fmt.Errorf("payload is required and cannot be empty")
	}
	return nil
}

func (p *UpdateTaskParams) ParseUID() (uuid.UUID, error) {
	return uuid.Parse(p.UID)
}

type PatchTaskParams struct {
	UID     string         `json:"uid"`
	Partial map[string]any `json:"partial"`
}

func (p *PatchTaskParams) Validate() error {
	if _, err := uuid.Parse(p.UID); err != nil {
		return fmt.Errorf("invalid uid: %w", err)
	}
	if p.Partial == nil || len(p.Partial) == 0 {
		return fmt.Errorf("partial is required and cannot be empty")
	}
	return nil
}

func (p *PatchTaskParams) ParseUID() (uuid.UUID, error) {
	return uuid.Parse(p.UID)
}

type DeleteTaskParams struct {
	UID string `json:"uid"`
}

func (p *DeleteTaskParams) Validate() error {
	if _, err := uuid.Parse(p.UID); err != nil {
		return fmt.Errorf("invalid uid: %w", err)
	}
	return nil
}

func (p *DeleteTaskParams) ParseUID() (uuid.UUID, error) {
	return uuid.Parse(p.UID)
}

type ArchiveTaskParams struct {
	UID string `json:"uid"`
}

func (p *ArchiveTaskParams) Validate() error {
	if _, err := uuid.Parse(p.UID); err != nil {
		return fmt.Errorf("invalid uid: %w", err)
	}
	return nil
}

func (p *ArchiveTaskParams) ParseUID() (uuid.UUID, error) {
	return uuid.Parse(p.UID)
}

type ProcessTaskParams struct {
	UID      string         `json:"uid"`
	Action   string         `json:"action"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

func (p *ProcessTaskParams) Validate() error {
	if _, err := uuid.Parse(p.UID); err != nil {
		return fmt.Errorf("invalid uid: %w", err)
	}
	allowedActions := map[string]bool{
		"complete":   true,
		"reopen":     true,
		"prioritize": true,
		"assign":     true,
	}
	if !allowedActions[p.Action] {
		return fmt.Errorf("invalid action: %s", p.Action)
	}
	return nil
}

func (p *ProcessTaskParams) ParseUID() (uuid.UUID, error) {
	return uuid.Parse(p.UID)
}
