"""
MCP tools for Note management.

Provides tools for creating, reading, updating, deleting, and listing notes
via the ToolBridge Go REST API.
"""

from typing import Annotated, List, Optional, Any, Dict, Union
import json

from pydantic import BaseModel, Field, field_validator
from loguru import logger

from toolbridge_mcp.async_client import get_client
from toolbridge_mcp.utils.requests import call_get, call_post, call_put, call_patch, call_delete
from toolbridge_mcp.mcp_instance import mcp


# Pydantic models matching Go API responses


class Note(BaseModel):
    """Individual note with version and metadata."""

    uid: str
    version: int
    updated_at: str = Field(alias="updatedAt")
    deleted_at: Optional[str] = Field(default=None, alias="deletedAt")
    payload: Dict[str, Any]

    class Config:
        populate_by_name = True


class NotesListResponse(BaseModel):
    """Paginated list of notes."""

    items: List[Note]
    next_cursor: Optional[str] = Field(default=None, alias="nextCursor")

    class Config:
        populate_by_name = True


# MCP Tool Definitions


@mcp.tool()
async def list_notes(
    limit: Annotated[
        int, Field(ge=1, le=1000, description="Maximum number of notes to return")
    ] = 100,
    cursor: Annotated[
        Optional[str], Field(description="Pagination cursor from previous response")
    ] = None,
    include_deleted: Annotated[bool, Field(description="Include soft-deleted notes")] = False,
) -> NotesListResponse:
    """
    List notes with cursor-based pagination.

    Returns notes for the authenticated tenant. Supports filtering and pagination.
    Use the next_cursor from the response to fetch additional pages.

    Args:
        limit: Maximum number of notes to return (1-1000, default 100)
        cursor: Optional pagination cursor from previous response
        include_deleted: Whether to include soft-deleted notes (default False)

    Returns:
        NotesListResponse containing items and optional next_cursor

    Examples:
        # List first 10 notes
        >>> await list_notes(limit=10)

        # List next page
        >>> await list_notes(limit=10, cursor="...")

        # Include deleted notes
        >>> await list_notes(include_deleted=True)
    """
    async with get_client() as client:
        params = {"limit": limit}
        if cursor:
            params["cursor"] = cursor
        if include_deleted:
            params["includeDeleted"] = "true"

        logger.info(
            f"Listing notes: limit={limit}, cursor={cursor}, include_deleted={include_deleted}"
        )
        response = await call_get(client, "/v1/notes", params=params)
        data = response.json()

        return NotesListResponse(**data)


@mcp.tool()
async def get_note(
    uid: Annotated[str, Field(description="Unique identifier of the note")],
    include_deleted: Annotated[bool, Field(description="Allow retrieving deleted notes")] = False,
) -> Note:
    """
    Retrieve a single note by UID.

    Args:
        uid: Unique identifier of the note (UUID format)
        include_deleted: Whether to allow retrieving soft-deleted notes

    Returns:
        Note object with full details

    Raises:
        httpx.HTTPStatusError: 404 if note not found, 410 if deleted (unless include_deleted=True)

    Examples:
        # Get a specific note
        >>> await get_note("c1d9b7dc-a1b2-4c3d-9e8f-7a6b5c4d3e2f")

        # Get a deleted note
        >>> await get_note("c1d9b7dc-...", include_deleted=True)
    """
    async with get_client() as client:
        params = {}
        if include_deleted:
            params["includeDeleted"] = "true"

        logger.info(f"Getting note: uid={uid}")
        response = await call_get(client, f"/v1/notes/{uid}", params=params)
        data = response.json()

        return Note(**data)


@mcp.tool()
async def create_note(
    title: Annotated[str, Field(description="Note title")],
    content: Annotated[str, Field(description="Note content (markdown supported)")],
    tags: Annotated[
        Optional[Union[List[str], str]],
        Field(description="Optional tags for categorization (as list or JSON string)"),
    ] = None,
    additional_fields: Annotated[
        Optional[Union[Dict[str, Any], str]],
        Field(description="Additional custom fields (as dict or JSON string)"),
    ] = None,
) -> Note:
    """
    Create a new note.

    The server automatically generates a UID for the note. Returns the created
    note with version=1 and timestamps.

    Args:
        title: Note title
        content: Note content (supports markdown)
        tags: Optional list of tags for categorization (can be a list or JSON-encoded string)
        additional_fields: Optional dictionary of additional custom fields (can be a dict or JSON-encoded string)

    Returns:
        Note object with server-generated UID and metadata

    Examples:
        # Simple note
        >>> await create_note(title="Meeting Notes", content="Discussed project timeline")

        # Note with tags (as list)
        >>> await create_note(
        ...     title="Research Ideas",
        ...     content="Potential topics for investigation",
        ...     tags=["research", "ideas"]
        ... )

        # Note with tags (as JSON string)
        >>> await create_note(
        ...     title="Research Ideas",
        ...     content="Potential topics for investigation",
        ...     tags='["research", "ideas"]'
        ... )

        # Note with custom fields
        >>> await create_note(
        ...     title="Task",
        ...     content="Complete documentation",
        ...     additional_fields={"priority": "high", "due_date": "2025-12-01"}
        ... )
    """
    async with get_client() as client:
        payload: Dict[str, Any] = {
            "title": title,
            "content": content,
        }

        # Parse tags if it's a JSON string
        if tags:
            if isinstance(tags, str):
                try:
                    tags = json.loads(tags)
                except json.JSONDecodeError as e:
                    raise ValueError(f"Invalid JSON string for tags: {e}")
            payload["tags"] = tags

        # Parse additional_fields if it's a JSON string
        if additional_fields:
            if isinstance(additional_fields, str):
                try:
                    additional_fields = json.loads(additional_fields)
                except json.JSONDecodeError as e:
                    raise ValueError(f"Invalid JSON string for additional_fields: {e}")
            payload.update(additional_fields)

        logger.info(f"Creating note: title={title}")
        response = await call_post(client, "/v1/notes", json=payload)
        data = response.json()

        return Note(**data)


@mcp.tool()
async def update_note(
    uid: Annotated[str, Field(description="Unique identifier of the note")],
    title: Annotated[str, Field(description="Note title")],
    content: Annotated[str, Field(description="Note content")],
    if_match: Annotated[
        Optional[int], Field(description="Expected version for optimistic locking")
    ] = None,
    additional_fields: Annotated[
        Optional[Union[Dict[str, Any], str]],
        Field(description="Additional custom fields (as dict or JSON string)"),
    ] = None,
) -> Note:
    """
    Replace a note (full update).

    Replaces the entire note payload. For partial updates, use patch_note instead.
    Supports optimistic locking via if_match parameter.

    Args:
        uid: Unique identifier of the note
        title: New note title
        content: New note content
        if_match: Expected version number (returns 409 if mismatch)
        additional_fields: Additional custom fields to include (can be a dict or JSON-encoded string)

    Returns:
        Updated note with incremented version

    Raises:
        httpx.HTTPStatusError: 409 if version mismatch, 404 if not found

    Examples:
        # Simple update
        >>> await update_note("c1d9b7dc-...", title="Updated Title", content="New content")

        # Update with optimistic locking
        >>> await update_note("c1d9b7dc-...", title="...", content="...", if_match=3)
    """
    async with get_client() as client:
        payload: Dict[str, Any] = {
            "uid": uid,
            "title": title,
            "content": content,
        }

        # Parse additional_fields if it's a JSON string
        if additional_fields:
            if isinstance(additional_fields, str):
                try:
                    additional_fields = json.loads(additional_fields)
                except json.JSONDecodeError as e:
                    raise ValueError(f"Invalid JSON string for additional_fields: {e}")
            payload.update(additional_fields)

        logger.info(f"Updating note: uid={uid}, if_match={if_match}")
        response = await call_put(client, f"/v1/notes/{uid}", json=payload, if_match=if_match)
        data = response.json()

        return Note(**data)


@mcp.tool()
async def patch_note(
    uid: Annotated[str, Field(description="Unique identifier of the note")],
    updates: Annotated[
        Union[Dict[str, Any], str],
        Field(description="Fields to update (partial, as dict or JSON string)"),
    ],
) -> Note:
    """
    Partially update a note.

    Only specified fields are updated; other fields remain unchanged.
    Use this for targeted updates when you don't want to replace the entire note.

    Args:
        uid: Unique identifier of the note
        updates: Dictionary of fields to update (can be a dict or JSON-encoded string, e.g., {"title": "New Title"})

    Returns:
        Updated note with incremented version

    Examples:
        # Update only title
        >>> await patch_note("c1d9b7dc-...", {"title": "New Title"})

        # Update multiple fields
        >>> await patch_note("c1d9b7dc-...", {
        ...     "title": "Updated",
        ...     "tags": ["important", "urgent"]
        ... })
    """
    async with get_client() as client:
        # Parse updates if it's a JSON string
        if isinstance(updates, str):
            try:
                updates = json.loads(updates)
            except json.JSONDecodeError as e:
                raise ValueError(f"Invalid JSON string for updates: {e}")

        logger.info(f"Patching note: uid={uid}, updates={list(updates.keys())}")
        response = await call_patch(client, f"/v1/notes/{uid}", json=updates)
        data = response.json()

        return Note(**data)


@mcp.tool()
async def delete_note(
    uid: Annotated[str, Field(description="Unique identifier of the note")],
) -> Note:
    """
    Soft delete a note.

    Marks the note as deleted (sets deletedAt timestamp) but doesn't remove it
    from the database. Deleted notes can still be retrieved with include_deleted=True.

    Args:
        uid: Unique identifier of the note

    Returns:
        Deleted note with deletedAt timestamp set

    Examples:
        >>> await delete_note("c1d9b7dc-a1b2-4c3d-9e8f-7a6b5c4d3e2f")
    """
    async with get_client() as client:
        logger.info(f"Deleting note: uid={uid}")
        response = await call_delete(client, f"/v1/notes/{uid}")
        data = response.json()

        return Note(**data)


@mcp.tool()
async def archive_note(
    uid: Annotated[str, Field(description="Unique identifier of the note")],
) -> Note:
    """
    Archive a note.

    Sets the note's status to "archived". Archived notes remain accessible
    but are typically hidden from default views.

    Args:
        uid: Unique identifier of the note

    Returns:
        Note with status="archived"

    Examples:
        >>> await archive_note("c1d9b7dc-a1b2-4c3d-9e8f-7a6b5c4d3e2f")
    """
    async with get_client() as client:
        logger.info(f"Archiving note: uid={uid}")
        response = await call_post(client, f"/v1/notes/{uid}/archive", json={})
        data = response.json()

        return Note(**data)


@mcp.tool()
async def process_note(
    uid: Annotated[str, Field(description="Unique identifier of the note")],
    action: Annotated[str, Field(description="Action to perform (pin, unpin, archive, unarchive)")],
    metadata: Annotated[
        Optional[Union[Dict[str, Any], str]],
        Field(description="Optional action metadata (as dict or JSON string)"),
    ] = None,
) -> Note:
    """
    Process a note action (state machine transition).

    Supported actions:
    - pin: Mark note as pinned
    - unpin: Remove pinned status
    - archive: Archive the note
    - unarchive: Unarchive the note

    Args:
        uid: Unique identifier of the note
        action: Action to perform
        metadata: Optional metadata for the action (can be a dict or JSON-encoded string)

    Returns:
        Updated note after action is applied

    Examples:
        # Pin a note
        >>> await process_note("c1d9b7dc-...", "pin")

        # Archive with metadata
        >>> await process_note("c1d9b7dc-...", "archive", {"reason": "completed"})
    """
    async with get_client() as client:
        payload = {"action": action}

        # Parse metadata if it's a JSON string
        if metadata:
            if isinstance(metadata, str):
                try:
                    metadata = json.loads(metadata)
                except json.JSONDecodeError as e:
                    raise ValueError(f"Invalid JSON string for metadata: {e}")
            payload["metadata"] = metadata

        logger.info(f"Processing note: uid={uid}, action={action}")
        response = await call_post(client, f"/v1/notes/{uid}/process", json=payload)
        data = response.json()

        return Note(**data)
