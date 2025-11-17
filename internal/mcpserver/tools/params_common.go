package tools

import (
	"fmt"
	"github.com/google/uuid"
)

// Comments
type ListCommentsParams struct {
	Cursor         *string `json:"cursor,omitempty"`
	Limit          *int    `json:"limit,omitempty"`
	IncludeDeleted bool    `json:"includeDeleted,omitempty"`
}

func (p *ListCommentsParams) Validate() error {
	if p.Limit != nil && (*p.Limit < 1 || *p.Limit > 1000) {
		return fmt.Errorf("limit must be between 1 and 1000")
	}
	return nil
}

type GetCommentParams struct {
	UID            string `json:"uid"`
	IncludeDeleted bool   `json:"includeDeleted,omitempty"`
}

func (p *GetCommentParams) Validate() error {
	if _, err := uuid.Parse(p.UID); err != nil {
		return fmt.Errorf("invalid uid: %w", err)
	}
	return nil
}

func (p *GetCommentParams) ParseUID() (uuid.UUID, error) {
	return uuid.Parse(p.UID)
}

type CreateCommentParams struct {
	UID     *string        `json:"uid,omitempty"`
	Payload map[string]any `json:"payload"`
}

func (p *CreateCommentParams) Validate() error {
	if p.Payload == nil || len(p.Payload) == 0 {
		return fmt.Errorf("payload is required and cannot be empty")
	}
	if _, hasParentUID := p.Payload["parentUid"]; !hasParentUID {
		return fmt.Errorf("payload must include parentUid (note or task)")
	}
	if _, hasParentKind := p.Payload["parentKind"]; !hasParentKind {
		return fmt.Errorf("payload must include parentKind (note or task)")
	}
	if p.UID != nil {
		if _, err := uuid.Parse(*p.UID); err != nil {
			return fmt.Errorf("invalid uid: %w", err)
		}
	}
	return nil
}

func (p *CreateCommentParams) ParseUID() (*uuid.UUID, error) {
	if p.UID == nil {
		return nil, nil
	}
	uid, err := uuid.Parse(*p.UID)
	if err != nil {
		return nil, err
	}
	return &uid, nil
}

// Chats
type ListChatsParams struct {
	Cursor         *string `json:"cursor,omitempty"`
	Limit          *int    `json:"limit,omitempty"`
	IncludeDeleted bool    `json:"includeDeleted,omitempty"`
}

func (p *ListChatsParams) Validate() error {
	if p.Limit != nil && (*p.Limit < 1 || *p.Limit > 1000) {
		return fmt.Errorf("limit must be between 1 and 1000")
	}
	return nil
}

type GetChatParams struct {
	UID            string `json:"uid"`
	IncludeDeleted bool   `json:"includeDeleted,omitempty"`
}

func (p *GetChatParams) Validate() error {
	if _, err := uuid.Parse(p.UID); err != nil {
		return fmt.Errorf("invalid uid: %w", err)
	}
	return nil
}

func (p *GetChatParams) ParseUID() (uuid.UUID, error) {
	return uuid.Parse(p.UID)
}

type CreateChatParams struct {
	UID     *string        `json:"uid,omitempty"`
	Payload map[string]any `json:"payload"`
}

func (p *CreateChatParams) Validate() error {
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

func (p *CreateChatParams) ParseUID() (*uuid.UUID, error) {
	if p.UID == nil {
		return nil, nil
	}
	uid, err := uuid.Parse(*p.UID)
	if err != nil {
		return nil, err
	}
	return &uid, nil
}

// ChatMessages
type ListChatMessagesParams struct {
	Cursor         *string `json:"cursor,omitempty"`
	Limit          *int    `json:"limit,omitempty"`
	IncludeDeleted bool    `json:"includeDeleted,omitempty"`
}

func (p *ListChatMessagesParams) Validate() error {
	if p.Limit != nil && (*p.Limit < 1 || *p.Limit > 1000) {
		return fmt.Errorf("limit must be between 1 and 1000")
	}
	return nil
}

type GetChatMessageParams struct {
	UID            string `json:"uid"`
	IncludeDeleted bool   `json:"includeDeleted,omitempty"`
}

func (p *GetChatMessageParams) Validate() error {
	if _, err := uuid.Parse(p.UID); err != nil {
		return fmt.Errorf("invalid uid: %w", err)
	}
	return nil
}

func (p *GetChatMessageParams) ParseUID() (uuid.UUID, error) {
	return uuid.Parse(p.UID)
}

type CreateChatMessageParams struct {
	UID     *string        `json:"uid,omitempty"`
	Payload map[string]any `json:"payload"`
}

func (p *CreateChatMessageParams) Validate() error {
	if p.Payload == nil || len(p.Payload) == 0 {
		return fmt.Errorf("payload is required and cannot be empty")
	}
	if _, hasChatUID := p.Payload["chatUid"]; !hasChatUID {
		return fmt.Errorf("payload must include chatUid")
	}
	if p.UID != nil {
		if _, err := uuid.Parse(*p.UID); err != nil {
			return fmt.Errorf("invalid uid: %w", err)
		}
	}
	return nil
}

func (p *CreateChatMessageParams) ParseUID() (*uuid.UUID, error) {
	if p.UID == nil {
		return nil, nil
	}
	uid, err := uuid.Parse(*p.UID)
	if err != nil {
		return nil, err
	}
	return &uid, nil
}

// Generic params for update/patch/delete/archive
type GenericUpdateParams struct {
	UID     string         `json:"uid"`
	Payload map[string]any `json:"payload"`
	Version *int           `json:"version,omitempty"`
}

func (p *GenericUpdateParams) Validate() error {
	if _, err := uuid.Parse(p.UID); err != nil {
		return fmt.Errorf("invalid uid: %w", err)
	}
	if p.Payload == nil || len(p.Payload) == 0 {
		return fmt.Errorf("payload is required and cannot be empty")
	}
	return nil
}

func (p *GenericUpdateParams) ParseUID() (uuid.UUID, error) {
	return uuid.Parse(p.UID)
}

type GenericPatchParams struct {
	UID     string         `json:"uid"`
	Partial map[string]any `json:"partial"`
}

func (p *GenericPatchParams) Validate() error {
	if _, err := uuid.Parse(p.UID); err != nil {
		return fmt.Errorf("invalid uid: %w", err)
	}
	if p.Partial == nil || len(p.Partial) == 0 {
		return fmt.Errorf("partial is required and cannot be empty")
	}
	return nil
}

func (p *GenericPatchParams) ParseUID() (uuid.UUID, error) {
	return uuid.Parse(p.UID)
}

type GenericDeleteParams struct {
	UID string `json:"uid"`
}

func (p *GenericDeleteParams) Validate() error {
	if _, err := uuid.Parse(p.UID); err != nil {
		return fmt.Errorf("invalid uid: %w", err)
	}
	return nil
}

func (p *GenericDeleteParams) ParseUID() (uuid.UUID, error) {
	return uuid.Parse(p.UID)
}

type GenericArchiveParams struct {
	UID string `json:"uid"`
}

func (p *GenericArchiveParams) Validate() error {
	if _, err := uuid.Parse(p.UID); err != nil {
		return fmt.Errorf("invalid uid: %w", err)
	}
	return nil
}

func (p *GenericArchiveParams) ParseUID() (uuid.UUID, error) {
	return uuid.Parse(p.UID)
}

type GenericProcessParams struct {
	UID      string         `json:"uid"`
	Action   string         `json:"action"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

func (p *GenericProcessParams) Validate(allowedActions map[string]bool) error {
	if _, err := uuid.Parse(p.UID); err != nil {
		return fmt.Errorf("invalid uid: %w", err)
	}
	if !allowedActions[p.Action] {
		return fmt.Errorf("invalid action: %s", p.Action)
	}
	return nil
}

func (p *GenericProcessParams) ParseUID() (uuid.UUID, error) {
	return uuid.Parse(p.UID)
}
