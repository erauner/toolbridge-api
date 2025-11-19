"""
MCP tools for Task management.

Provides tools for creating, reading, updating, deleting, and listing tasks
via the ToolBridge Go REST API.
"""

from typing import Annotated, List, Optional, Any, Dict

from pydantic import BaseModel, Field
from loguru import logger

from toolbridge_mcp.async_client import get_client
from toolbridge_mcp.utils.requests import call_get, call_post, call_put, call_patch, call_delete
from toolbridge_mcp.mcp_instance import mcp


# Pydantic models matching Go API responses


class Task(BaseModel):
    """Individual task with version and metadata."""
    
    uid: str
    version: int
    updated_at: str = Field(alias="updatedAt")
    deleted_at: Optional[str] = Field(default=None, alias="deletedAt")
    payload: Dict[str, Any]
    
    class Config:
        populate_by_name = True


class TasksListResponse(BaseModel):
    """Paginated list of tasks."""
    
    items: List[Task]
    next_cursor: Optional[str] = Field(default=None, alias="nextCursor")
    
    class Config:
        populate_by_name = True


# MCP Tool Definitions


@mcp.tool()
async def list_tasks(
    limit: Annotated[int, Field(ge=1, le=1000, description="Maximum number of tasks to return")] = 100,
    cursor: Annotated[Optional[str], Field(description="Pagination cursor from previous response")] = None,
    include_deleted: Annotated[bool, Field(description="Include soft-deleted tasks")] = False,
) -> TasksListResponse:
    """
    List tasks with cursor-based pagination.
    
    Returns tasks for the authenticated tenant. Supports filtering and pagination.
    Use the next_cursor from the response to fetch additional pages.
    
    Args:
        limit: Maximum number of tasks to return (1-1000, default 100)
        cursor: Optional pagination cursor from previous response
        include_deleted: Whether to include soft-deleted tasks (default False)
    
    Returns:
        TasksListResponse containing items and optional next_cursor
    
    Examples:
        # List first 10 tasks
        >>> await list_tasks(limit=10)
        
        # List next page
        >>> await list_tasks(limit=10, cursor="...")
        
        # Include deleted tasks
        >>> await list_tasks(include_deleted=True)
    """
    async with get_client() as client:
        params = {"limit": limit}
        if cursor:
            params["cursor"] = cursor
        if include_deleted:
            params["includeDeleted"] = "true"
        
        logger.info(f"Listing tasks: limit={limit}, cursor={cursor}, include_deleted={include_deleted}")
        response = await call_get(client, "/v1/tasks", params=params)
        data = response.json()
        
        return TasksListResponse(**data)


@mcp.tool()
async def get_task(
    uid: Annotated[str, Field(description="Unique identifier of the task")],
    include_deleted: Annotated[bool, Field(description="Allow retrieving deleted tasks")] = False,
) -> Task:
    """
    Retrieve a single task by UID.
    
    Args:
        uid: Unique identifier of the task (UUID format)
        include_deleted: Whether to allow retrieving soft-deleted tasks
    
    Returns:
        Task object with full details
    
    Raises:
        httpx.HTTPStatusError: 404 if task not found, 410 if deleted (unless include_deleted=True)
    
    Examples:
        # Get a specific task
        >>> await get_task("c1d9b7dc-a1b2-4c3d-9e8f-7a6b5c4d3e2f")
        
        # Get a deleted task
        >>> await get_task("c1d9b7dc-...", include_deleted=True)
    """
    async with get_client() as client:
        params = {}
        if include_deleted:
            params["includeDeleted"] = "true"
        
        logger.info(f"Getting task: uid={uid}")
        response = await call_get(client, f"/v1/tasks/{uid}", params=params)
        data = response.json()
        
        return Task(**data)


@mcp.tool()
async def create_task(
    title: Annotated[str, Field(description="Task title")],
    description: Annotated[Optional[str], Field(description="Task description")] = None,
    status: Annotated[Optional[str], Field(description="Task status (todo, in_progress, done)")] = None,
    priority: Annotated[Optional[str], Field(description="Task priority (low, medium, high)")] = None,
    due_date: Annotated[Optional[str], Field(description="Due date (ISO 8601 format)")] = None,
    tags: Annotated[Optional[List[str]], Field(description="Optional tags for categorization")] = None,
    additional_fields: Annotated[Optional[Dict[str, Any]], Field(description="Additional custom fields")] = None,
) -> Task:
    """
    Create a new task.
    
    The server automatically generates a UID for the task. Returns the created
    task with version=1 and timestamps.
    
    Args:
        title: Task title
        description: Optional task description
        status: Task status (todo, in_progress, done)
        priority: Task priority (low, medium, high)
        due_date: Due date in ISO 8601 format (e.g., "2025-12-31T23:59:59Z")
        tags: Optional list of tags for categorization
        additional_fields: Optional dictionary of additional custom fields
    
    Returns:
        Task object with server-generated UID and metadata
    
    Examples:
        # Simple task
        >>> await create_task(title="Complete documentation")
        
        # Task with status and priority
        >>> await create_task(
        ...     title="Fix bug #123",
        ...     description="Authentication error in production",
        ...     status="todo",
        ...     priority="high"
        ... )
        
        # Task with due date and tags
        >>> await create_task(
        ...     title="Q4 Planning",
        ...     due_date="2025-12-15T17:00:00Z",
        ...     tags=["planning", "q4"]
        ... )
    """
    async with get_client() as client:
        payload: Dict[str, Any] = {"title": title}
        
        if description:
            payload["description"] = description
        if status:
            payload["status"] = status
        if priority:
            payload["priority"] = priority
        if due_date:
            payload["dueDate"] = due_date
        if tags:
            payload["tags"] = tags
        
        if additional_fields:
            payload.update(additional_fields)
        
        logger.info(f"Creating task: title={title}")
        response = await call_post(client, "/v1/tasks", json=payload)
        data = response.json()
        
        return Task(**data)


@mcp.tool()
async def update_task(
    uid: Annotated[str, Field(description="Unique identifier of the task")],
    title: Annotated[str, Field(description="Task title")],
    description: Annotated[Optional[str], Field(description="Task description")] = None,
    status: Annotated[Optional[str], Field(description="Task status")] = None,
    if_match: Annotated[Optional[int], Field(description="Expected version for optimistic locking")] = None,
    additional_fields: Annotated[Optional[Dict[str, Any]], Field(description="Additional custom fields")] = None,
) -> Task:
    """
    Replace a task (full update).
    
    Replaces the entire task payload. For partial updates, use patch_task instead.
    Supports optimistic locking via if_match parameter.
    
    Args:
        uid: Unique identifier of the task
        title: New task title
        description: Task description
        status: Task status
        if_match: Expected version number (returns 409 if mismatch)
        additional_fields: Additional custom fields to include
    
    Returns:
        Updated task with incremented version
    
    Raises:
        httpx.HTTPStatusError: 409 if version mismatch, 404 if not found
    
    Examples:
        # Simple update
        >>> await update_task("c1d9b7dc-...", title="Updated Title", description="New description")
        
        # Update with optimistic locking
        >>> await update_task("c1d9b7dc-...", title="...", status="done", if_match=3)
    """
    async with get_client() as client:
        payload: Dict[str, Any] = {
            "uid": uid,
            "title": title,
        }
        
        if description:
            payload["description"] = description
        if status:
            payload["status"] = status
        
        if additional_fields:
            payload.update(additional_fields)
        
        logger.info(f"Updating task: uid={uid}, if_match={if_match}")
        response = await call_put(client, f"/v1/tasks/{uid}", json=payload, if_match=if_match)
        data = response.json()
        
        return Task(**data)


@mcp.tool()
async def patch_task(
    uid: Annotated[str, Field(description="Unique identifier of the task")],
    updates: Annotated[Dict[str, Any], Field(description="Fields to update (partial)")],
) -> Task:
    """
    Partially update a task.
    
    Only specified fields are updated; other fields remain unchanged.
    Use this for targeted updates when you don't want to replace the entire task.
    
    Args:
        uid: Unique identifier of the task
        updates: Dictionary of fields to update (e.g., {"status": "done"})
    
    Returns:
        Updated task with incremented version
    
    Examples:
        # Update only status
        >>> await patch_task("c1d9b7dc-...", {"status": "done"})
        
        # Update multiple fields
        >>> await patch_task("c1d9b7dc-...", {
        ...     "priority": "high",
        ...     "status": "in_progress"
        ... })
    """
    async with get_client() as client:
        logger.info(f"Patching task: uid={uid}, updates={list(updates.keys())}")
        response = await call_patch(client, f"/v1/tasks/{uid}", json=updates)
        data = response.json()
        
        return Task(**data)


@mcp.tool()
async def delete_task(
    uid: Annotated[str, Field(description="Unique identifier of the task")],
) -> Task:
    """
    Soft delete a task.
    
    Marks the task as deleted (sets deletedAt timestamp) but doesn't remove it
    from the database. Deleted tasks can still be retrieved with include_deleted=True.
    
    Args:
        uid: Unique identifier of the task
    
    Returns:
        Deleted task with deletedAt timestamp set
    
    Examples:
        >>> await delete_task("c1d9b7dc-a1b2-4c3d-9e8f-7a6b5c4d3e2f")
    """
    async with get_client() as client:
        logger.info(f"Deleting task: uid={uid}")
        response = await call_delete(client, f"/v1/tasks/{uid}")
        data = response.json()
        
        return Task(**data)


@mcp.tool()
async def archive_task(
    uid: Annotated[str, Field(description="Unique identifier of the task")],
) -> Task:
    """
    Archive a task.
    
    Sets the task's status to "archived". Archived tasks remain accessible
    but are typically hidden from default views.
    
    Args:
        uid: Unique identifier of the task
    
    Returns:
        Task with status="archived"
    
    Examples:
        >>> await archive_task("c1d9b7dc-a1b2-4c3d-9e8f-7a6b5c4d3e2f")
    """
    async with get_client() as client:
        logger.info(f"Archiving task: uid={uid}")
        response = await call_post(client, f"/v1/tasks/{uid}/archive", json={})
        data = response.json()
        
        return Task(**data)


@mcp.tool()
async def process_task(
    uid: Annotated[str, Field(description="Unique identifier of the task")],
    action: Annotated[str, Field(description="Action to perform (start, complete, reopen)")],
    metadata: Annotated[Optional[Dict[str, Any]], Field(description="Optional action metadata")] = None,
) -> Task:
    """
    Process a task action (state machine transition).
    
    Supported actions:
    - start: Mark task as in_progress
    - complete: Mark task as done
    - reopen: Reopen a completed task
    
    Args:
        uid: Unique identifier of the task
        action: Action to perform
        metadata: Optional metadata for the action
    
    Returns:
        Updated task after action is applied
    
    Examples:
        # Start a task
        >>> await process_task("c1d9b7dc-...", "start")
        
        # Complete with metadata
        >>> await process_task("c1d9b7dc-...", "complete", {"completed_by": "user@example.com"})
    """
    async with get_client() as client:
        payload = {"action": action}
        if metadata:
            payload["metadata"] = metadata
        
        logger.info(f"Processing task: uid={uid}, action={action}")
        response = await call_post(client, f"/v1/tasks/{uid}/process", json=payload)
        data = response.json()
        
        return Task(**data)
