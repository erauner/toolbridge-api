"""
MCP tools for Chat management.

Provides tools for creating, reading, updating, deleting, and listing chats
via the ToolBridge Go REST API.
"""

from typing import Annotated, List, Optional, Any, Dict

from pydantic import BaseModel, Field
from loguru import logger

from toolbridge_mcp.async_client import get_client
from toolbridge_mcp.utils.requests import call_get, call_post, call_put, call_patch, call_delete
from toolbridge_mcp.server import mcp


# Pydantic models matching Go API responses


class Chat(BaseModel):
    """Individual chat with version and metadata."""
    
    uid: str
    version: int
    updated_at: str = Field(alias="updatedAt")
    deleted_at: Optional[str] = Field(default=None, alias="deletedAt")
    payload: Dict[str, Any]
    
    class Config:
        populate_by_name = True


class ChatsListResponse(BaseModel):
    """Paginated list of chats."""
    
    items: List[Chat]
    next_cursor: Optional[str] = Field(default=None, alias="nextCursor")
    
    class Config:
        populate_by_name = True


# MCP Tool Definitions


@mcp.tool()
async def list_chats(
    limit: Annotated[int, Field(ge=1, le=1000, description="Maximum number of chats to return")] = 100,
    cursor: Annotated[Optional[str], Field(description="Pagination cursor from previous response")] = None,
    include_deleted: Annotated[bool, Field(description="Include soft-deleted chats")] = False,
) -> ChatsListResponse:
    """
    List chats with cursor-based pagination.
    
    Returns chats for the authenticated tenant. Supports filtering and pagination.
    Use the next_cursor from the response to fetch additional pages.
    
    Args:
        limit: Maximum number of chats to return (1-1000, default 100)
        cursor: Optional pagination cursor from previous response
        include_deleted: Whether to include soft-deleted chats (default False)
    
    Returns:
        ChatsListResponse containing items and optional next_cursor
    
    Examples:
        # List first 10 chats
        >>> await list_chats(limit=10)
        
        # List next page
        >>> await list_chats(limit=10, cursor="...")
        
        # Include deleted chats
        >>> await list_chats(include_deleted=True)
    """
    async with get_client() as client:
        params = {"limit": limit}
        if cursor:
            params["cursor"] = cursor
        if include_deleted:
            params["includeDeleted"] = "true"
        
        logger.info(f"Listing chats: limit={limit}, cursor={cursor}, include_deleted={include_deleted}")
        response = await call_get(client, "/v1/chats", params=params)
        data = response.json()
        
        return ChatsListResponse(**data)


@mcp.tool()
async def get_chat(
    uid: Annotated[str, Field(description="Unique identifier of the chat")],
    include_deleted: Annotated[bool, Field(description="Allow retrieving deleted chats")] = False,
) -> Chat:
    """
    Retrieve a single chat by UID.
    
    Args:
        uid: Unique identifier of the chat (UUID format)
        include_deleted: Whether to allow retrieving soft-deleted chats
    
    Returns:
        Chat object with full details
    
    Raises:
        httpx.HTTPStatusError: 404 if chat not found, 410 if deleted (unless include_deleted=True)
    
    Examples:
        # Get a specific chat
        >>> await get_chat("c1d9b7dc-a1b2-4c3d-9e8f-7a6b5c4d3e2f")
        
        # Get a deleted chat
        >>> await get_chat("c1d9b7dc-...", include_deleted=True)
    """
    async with get_client() as client:
        params = {}
        if include_deleted:
            params["includeDeleted"] = "true"
        
        logger.info(f"Getting chat: uid={uid}")
        response = await call_get(client, f"/v1/chats/{uid}", params=params)
        data = response.json()
        
        return Chat(**data)


@mcp.tool()
async def create_chat(
    title: Annotated[str, Field(description="Chat title")],
    description: Annotated[Optional[str], Field(description="Chat description")] = None,
    participants: Annotated[Optional[List[str]], Field(description="List of participant IDs")] = None,
    archived: Annotated[bool, Field(description="Whether chat is archived")] = False,
    tags: Annotated[Optional[List[str]], Field(description="Optional tags for categorization")] = None,
    additional_fields: Annotated[Optional[Dict[str, Any]], Field(description="Additional custom fields")] = None,
) -> Chat:
    """
    Create a new chat.
    
    The server automatically generates a UID for the chat. Returns the created
    chat with version=1 and timestamps.
    
    Args:
        title: Chat title
        description: Optional chat description
        participants: Optional list of participant IDs
        archived: Whether the chat should be archived (default False)
        tags: Optional list of tags for categorization
        additional_fields: Optional dictionary of additional custom fields
    
    Returns:
        Chat object with server-generated UID and metadata
    
    Examples:
        # Simple chat
        >>> await create_chat(title="Project Discussion")
        
        # Chat with participants
        >>> await create_chat(
        ...     title="Team Standup",
        ...     description="Daily standup meeting",
        ...     participants=["user1", "user2", "user3"]
        ... )
        
        # Chat with tags
        >>> await create_chat(
        ...     title="Q4 Planning",
        ...     tags=["planning", "q4", "strategy"]
        ... )
    """
    async with get_client() as client:
        payload: Dict[str, Any] = {
            "title": title,
            "archived": archived,
        }
        
        if description:
            payload["description"] = description
        if participants:
            payload["participants"] = participants
        if tags:
            payload["tags"] = tags
        
        if additional_fields:
            payload.update(additional_fields)
        
        logger.info(f"Creating chat: title={title}")
        response = await call_post(client, "/v1/chats", json=payload)
        data = response.json()
        
        return Chat(**data)


@mcp.tool()
async def update_chat(
    uid: Annotated[str, Field(description="Unique identifier of the chat")],
    title: Annotated[str, Field(description="Chat title")],
    description: Annotated[Optional[str], Field(description="Chat description")] = None,
    if_match: Annotated[Optional[int], Field(description="Expected version for optimistic locking")] = None,
    additional_fields: Annotated[Optional[Dict[str, Any]], Field(description="Additional custom fields")] = None,
) -> Chat:
    """
    Replace a chat (full update).
    
    Replaces the entire chat payload. For partial updates, use patch_chat instead.
    Supports optimistic locking via if_match parameter.
    
    Args:
        uid: Unique identifier of the chat
        title: New chat title
        description: Chat description
        if_match: Expected version number (returns 409 if mismatch)
        additional_fields: Additional custom fields to include
    
    Returns:
        Updated chat with incremented version
    
    Raises:
        httpx.HTTPStatusError: 409 if version mismatch, 404 if not found
    
    Examples:
        # Simple update
        >>> await update_chat("c1d9b7dc-...", title="Updated Title", description="New description")
        
        # Update with optimistic locking
        >>> await update_chat("c1d9b7dc-...", title="...", if_match=3)
    """
    async with get_client() as client:
        payload: Dict[str, Any] = {
            "uid": uid,
            "title": title,
        }
        
        if description:
            payload["description"] = description
        
        if additional_fields:
            payload.update(additional_fields)
        
        logger.info(f"Updating chat: uid={uid}, if_match={if_match}")
        response = await call_put(client, f"/v1/chats/{uid}", json=payload, if_match=if_match)
        data = response.json()
        
        return Chat(**data)


@mcp.tool()
async def patch_chat(
    uid: Annotated[str, Field(description="Unique identifier of the chat")],
    updates: Annotated[Dict[str, Any], Field(description="Fields to update (partial)")],
) -> Chat:
    """
    Partially update a chat.
    
    Only specified fields are updated; other fields remain unchanged.
    Use this for targeted updates when you don't want to replace the entire chat.
    
    Args:
        uid: Unique identifier of the chat
        updates: Dictionary of fields to update (e.g., {"title": "New Title"})
    
    Returns:
        Updated chat with incremented version
    
    Examples:
        # Update only title
        >>> await patch_chat("c1d9b7dc-...", {"title": "Updated Title"})
        
        # Update multiple fields
        >>> await patch_chat("c1d9b7dc-...", {
        ...     "title": "New Title",
        ...     "archived": true
        ... })
    """
    async with get_client() as client:
        logger.info(f"Patching chat: uid={uid}, updates={list(updates.keys())}")
        response = await call_patch(client, f"/v1/chats/{uid}", json=updates)
        data = response.json()
        
        return Chat(**data)


@mcp.tool()
async def delete_chat(
    uid: Annotated[str, Field(description="Unique identifier of the chat")],
) -> Chat:
    """
    Soft delete a chat.
    
    Marks the chat as deleted (sets deletedAt timestamp) but doesn't remove it
    from the database. Deleted chats can still be retrieved with include_deleted=True.
    
    Args:
        uid: Unique identifier of the chat
    
    Returns:
        Deleted chat with deletedAt timestamp set
    
    Examples:
        >>> await delete_chat("c1d9b7dc-a1b2-4c3d-9e8f-7a6b5c4d3e2f")
    """
    async with get_client() as client:
        logger.info(f"Deleting chat: uid={uid}")
        response = await call_delete(client, f"/v1/chats/{uid}")
        data = response.json()
        
        return Chat(**data)


@mcp.tool()
async def archive_chat(
    uid: Annotated[str, Field(description="Unique identifier of the chat")],
) -> Chat:
    """
    Archive a chat.
    
    Sets the chat's archived flag to true. Archived chats remain accessible
    but are typically hidden from default views.
    
    Args:
        uid: Unique identifier of the chat
    
    Returns:
        Chat with archived=true
    
    Examples:
        >>> await archive_chat("c1d9b7dc-a1b2-4c3d-9e8f-7a6b5c4d3e2f")
    """
    async with get_client() as client:
        logger.info(f"Archiving chat: uid={uid}")
        response = await call_post(client, f"/v1/chats/{uid}/archive", json={})
        data = response.json()
        
        return Chat(**data)


@mcp.tool()
async def process_chat(
    uid: Annotated[str, Field(description="Unique identifier of the chat")],
    action: Annotated[str, Field(description="Action to perform (resolve, reopen)")],
    metadata: Annotated[Optional[Dict[str, Any]], Field(description="Optional action metadata")] = None,
) -> Chat:
    """
    Process a chat action (state machine transition).
    
    Supported actions:
    - resolve: Mark chat as resolved
    - reopen: Reopen a resolved chat
    
    Args:
        uid: Unique identifier of the chat
        action: Action to perform
        metadata: Optional metadata for the action
    
    Returns:
        Updated chat after action is applied
    
    Examples:
        # Resolve a chat
        >>> await process_chat("c1d9b7dc-...", "resolve")
        
        # Reopen with metadata
        >>> await process_chat("c1d9b7dc-...", "reopen", {"reason": "issue persists"})
    """
    async with get_client() as client:
        payload = {"action": action}
        if metadata:
            payload["metadata"] = metadata
        
        logger.info(f"Processing chat: uid={uid}, action={action}")
        response = await call_post(client, f"/v1/chats/{uid}/process", json=payload)
        data = response.json()
        
        return Chat(**data)
