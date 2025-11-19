"""
MCP tools for Comment management.

Provides tools for creating, reading, updating, deleting, and listing comments
via the ToolBridge Go REST API. Comments are linked to parent entities (notes, tasks, etc.).
"""

from typing import Annotated, List, Optional, Any, Dict

from pydantic import BaseModel, Field
from loguru import logger

from toolbridge_mcp.async_client import get_client
from toolbridge_mcp.utils.requests import call_get, call_post, call_put, call_patch, call_delete
from toolbridge_mcp.mcp_instance import mcp


# Pydantic models matching Go API responses


class Comment(BaseModel):
    """Individual comment with version and metadata."""
    
    uid: str
    version: int
    updated_at: str = Field(alias="updatedAt")
    deleted_at: Optional[str] = Field(default=None, alias="deletedAt")
    payload: Dict[str, Any]
    
    class Config:
        populate_by_name = True


class CommentsListResponse(BaseModel):
    """Paginated list of comments."""
    
    items: List[Comment]
    next_cursor: Optional[str] = Field(default=None, alias="nextCursor")
    
    class Config:
        populate_by_name = True


# MCP Tool Definitions


@mcp.tool()
async def list_comments(
    limit: Annotated[int, Field(ge=1, le=1000, description="Maximum number of comments to return")] = 100,
    cursor: Annotated[Optional[str], Field(description="Pagination cursor from previous response")] = None,
    include_deleted: Annotated[bool, Field(description="Include soft-deleted comments")] = False,
) -> CommentsListResponse:
    """
    List comments with cursor-based pagination.
    
    Returns comments for the authenticated tenant. Supports filtering and pagination.
    Use the next_cursor from the response to fetch additional pages.
    
    Args:
        limit: Maximum number of comments to return (1-1000, default 100)
        cursor: Optional pagination cursor from previous response
        include_deleted: Whether to include soft-deleted comments (default False)
    
    Returns:
        CommentsListResponse containing items and optional next_cursor
    
    Examples:
        # List first 10 comments
        >>> await list_comments(limit=10)
        
        # List next page
        >>> await list_comments(limit=10, cursor="...")
        
        # Include deleted comments
        >>> await list_comments(include_deleted=True)
    """
    async with get_client() as client:
        params = {"limit": limit}
        if cursor:
            params["cursor"] = cursor
        if include_deleted:
            params["includeDeleted"] = "true"
        
        logger.info(f"Listing comments: limit={limit}, cursor={cursor}, include_deleted={include_deleted}")
        response = await call_get(client, "/v1/comments", params=params)
        data = response.json()
        
        return CommentsListResponse(**data)


@mcp.tool()
async def get_comment(
    uid: Annotated[str, Field(description="Unique identifier of the comment")],
    include_deleted: Annotated[bool, Field(description="Allow retrieving deleted comments")] = False,
) -> Comment:
    """
    Retrieve a single comment by UID.
    
    Args:
        uid: Unique identifier of the comment (UUID format)
        include_deleted: Whether to allow retrieving soft-deleted comments
    
    Returns:
        Comment object with full details
    
    Raises:
        httpx.HTTPStatusError: 404 if comment not found, 410 if deleted (unless include_deleted=True)
    
    Examples:
        # Get a specific comment
        >>> await get_comment("c1d9b7dc-a1b2-4c3d-9e8f-7a6b5c4d3e2f")
        
        # Get a deleted comment
        >>> await get_comment("c1d9b7dc-...", include_deleted=True)
    """
    async with get_client() as client:
        params = {}
        if include_deleted:
            params["includeDeleted"] = "true"
        
        logger.info(f"Getting comment: uid={uid}")
        response = await call_get(client, f"/v1/comments/{uid}", params=params)
        data = response.json()
        
        return Comment(**data)


@mcp.tool()
async def create_comment(
    content: Annotated[str, Field(description="Comment content")],
    parent_type: Annotated[str, Field(description="Type of parent entity (note, task, chat)")],
    parent_uid: Annotated[str, Field(description="UID of parent entity")],
    author: Annotated[Optional[str], Field(description="Comment author name")] = None,
    tags: Annotated[Optional[List[str]], Field(description="Optional tags for categorization")] = None,
    additional_fields: Annotated[Optional[Dict[str, Any]], Field(description="Additional custom fields")] = None,
) -> Comment:
    """
    Create a new comment.
    
    Comments are always linked to a parent entity (note, task, or chat).
    The server automatically generates a UID for the comment.
    
    Args:
        content: Comment content (markdown supported)
        parent_type: Type of parent entity (note, task, chat)
        parent_uid: UID of the parent entity
        author: Optional author name
        tags: Optional list of tags for categorization
        additional_fields: Optional dictionary of additional custom fields
    
    Returns:
        Comment object with server-generated UID and metadata
    
    Examples:
        # Comment on a note
        >>> await create_comment(
        ...     content="Great insight!",
        ...     parent_type="note",
        ...     parent_uid="abc123-..."
        ... )
        
        # Comment on a task with author
        >>> await create_comment(
        ...     content="Working on this now",
        ...     parent_type="task",
        ...     parent_uid="def456-...",
        ...     author="John Doe"
        ... )
        
        # Comment with tags
        >>> await create_comment(
        ...     content="Needs review",
        ...     parent_type="note",
        ...     parent_uid="ghi789-...",
        ...     tags=["review", "important"]
        ... )
    """
    async with get_client() as client:
        payload: Dict[str, Any] = {
            "content": content,
            "parentType": parent_type,
            "parentUid": parent_uid,
        }
        
        if author:
            payload["author"] = author
        if tags:
            payload["tags"] = tags
        
        if additional_fields:
            payload.update(additional_fields)
        
        logger.info(f"Creating comment: parent_type={parent_type}, parent_uid={parent_uid}")
        response = await call_post(client, "/v1/comments", json=payload)
        data = response.json()
        
        return Comment(**data)


@mcp.tool()
async def update_comment(
    uid: Annotated[str, Field(description="Unique identifier of the comment")],
    content: Annotated[str, Field(description="Comment content")],
    if_match: Annotated[Optional[int], Field(description="Expected version for optimistic locking")] = None,
    additional_fields: Annotated[Optional[Dict[str, Any]], Field(description="Additional custom fields")] = None,
) -> Comment:
    """
    Replace a comment (full update).
    
    Replaces the entire comment payload. For partial updates, use patch_comment instead.
    Supports optimistic locking via if_match parameter.
    
    Args:
        uid: Unique identifier of the comment
        content: New comment content
        if_match: Expected version number (returns 409 if mismatch)
        additional_fields: Additional custom fields to include
    
    Returns:
        Updated comment with incremented version
    
    Raises:
        httpx.HTTPStatusError: 409 if version mismatch, 404 if not found
    
    Examples:
        # Simple update
        >>> await update_comment("c1d9b7dc-...", content="Updated content")
        
        # Update with optimistic locking
        >>> await update_comment("c1d9b7dc-...", content="...", if_match=3)
    """
    async with get_client() as client:
        payload: Dict[str, Any] = {
            "uid": uid,
            "content": content,
        }
        
        if additional_fields:
            payload.update(additional_fields)
        
        logger.info(f"Updating comment: uid={uid}, if_match={if_match}")
        response = await call_put(client, f"/v1/comments/{uid}", json=payload, if_match=if_match)
        data = response.json()
        
        return Comment(**data)


@mcp.tool()
async def patch_comment(
    uid: Annotated[str, Field(description="Unique identifier of the comment")],
    updates: Annotated[Dict[str, Any], Field(description="Fields to update (partial)")],
) -> Comment:
    """
    Partially update a comment.
    
    Only specified fields are updated; other fields remain unchanged.
    Use this for targeted updates when you don't want to replace the entire comment.
    
    Args:
        uid: Unique identifier of the comment
        updates: Dictionary of fields to update (e.g., {"content": "Updated text"})
    
    Returns:
        Updated comment with incremented version
    
    Examples:
        # Update only content
        >>> await patch_comment("c1d9b7dc-...", {"content": "Revised comment"})
        
        # Update multiple fields
        >>> await patch_comment("c1d9b7dc-...", {
        ...     "content": "Updated",
        ...     "tags": ["revised"]
        ... })
    """
    async with get_client() as client:
        logger.info(f"Patching comment: uid={uid}, updates={list(updates.keys())}")
        response = await call_patch(client, f"/v1/comments/{uid}", json=updates)
        data = response.json()
        
        return Comment(**data)


@mcp.tool()
async def delete_comment(
    uid: Annotated[str, Field(description="Unique identifier of the comment")],
) -> Comment:
    """
    Soft delete a comment.
    
    Marks the comment as deleted (sets deletedAt timestamp) but doesn't remove it
    from the database. Deleted comments can still be retrieved with include_deleted=True.
    
    Args:
        uid: Unique identifier of the comment
    
    Returns:
        Deleted comment with deletedAt timestamp set
    
    Examples:
        >>> await delete_comment("c1d9b7dc-a1b2-4c3d-9e8f-7a6b5c4d3e2f")
    """
    async with get_client() as client:
        logger.info(f"Deleting comment: uid={uid}")
        response = await call_delete(client, f"/v1/comments/{uid}")
        data = response.json()
        
        return Comment(**data)


@mcp.tool()
async def archive_comment(
    uid: Annotated[str, Field(description="Unique identifier of the comment")],
) -> Comment:
    """
    Archive a comment.
    
    Sets the comment's status to "archived". Archived comments remain accessible
    but are typically hidden from default views.
    
    Args:
        uid: Unique identifier of the comment
    
    Returns:
        Comment with status="archived"
    
    Examples:
        >>> await archive_comment("c1d9b7dc-a1b2-4c3d-9e8f-7a6b5c4d3e2f")
    """
    async with get_client() as client:
        logger.info(f"Archiving comment: uid={uid}")
        response = await call_post(client, f"/v1/comments/{uid}/archive", json={})
        data = response.json()
        
        return Comment(**data)


@mcp.tool()
async def process_comment(
    uid: Annotated[str, Field(description="Unique identifier of the comment")],
    action: Annotated[str, Field(description="Action to perform (resolve, reopen)")],
    metadata: Annotated[Optional[Dict[str, Any]], Field(description="Optional action metadata")] = None,
) -> Comment:
    """
    Process a comment action (state machine transition).
    
    Supported actions:
    - resolve: Mark comment as resolved
    - reopen: Reopen a resolved comment
    
    Args:
        uid: Unique identifier of the comment
        action: Action to perform
        metadata: Optional metadata for the action
    
    Returns:
        Updated comment after action is applied
    
    Examples:
        # Resolve a comment
        >>> await process_comment("c1d9b7dc-...", "resolve")
        
        # Reopen with metadata
        >>> await process_comment("c1d9b7dc-...", "reopen", {"reason": "needs clarification"})
    """
    async with get_client() as client:
        payload = {"action": action}
        if metadata:
            payload["metadata"] = metadata
        
        logger.info(f"Processing comment: uid={uid}, action={action}")
        response = await call_post(client, f"/v1/comments/{uid}/process", json=payload)
        data = response.json()
        
        return Comment(**data)
