"""
MCP tools for Chat Message management.

Provides tools for creating, reading, updating, deleting, and listing chat messages
via the ToolBridge Go REST API. Chat messages are always linked to a parent chat.
"""

from typing import Annotated, List, Optional, Any, Dict, Union
import json

from pydantic import BaseModel, Field
from loguru import logger

from toolbridge_mcp.async_client import get_client
from toolbridge_mcp.utils.requests import call_get, call_post, call_put, call_patch, call_delete
from toolbridge_mcp.mcp_instance import mcp


# Pydantic models matching Go API responses


class ChatMessage(BaseModel):
    """Individual chat message with version and metadata."""
    
    uid: str
    version: int
    updated_at: str = Field(alias="updatedAt")
    deleted_at: Optional[str] = Field(default=None, alias="deletedAt")
    payload: Dict[str, Any]
    
    class Config:
        populate_by_name = True


class ChatMessagesListResponse(BaseModel):
    """Paginated list of chat messages."""
    
    items: List[ChatMessage]
    next_cursor: Optional[str] = Field(default=None, alias="nextCursor")
    
    class Config:
        populate_by_name = True


# MCP Tool Definitions


@mcp.tool()
async def list_chat_messages(
    limit: Annotated[int, Field(ge=1, le=1000, description="Maximum number of messages to return")] = 100,
    cursor: Annotated[Optional[str], Field(description="Pagination cursor from previous response")] = None,
    include_deleted: Annotated[bool, Field(description="Include soft-deleted messages")] = False,
) -> ChatMessagesListResponse:
    """
    List chat messages with cursor-based pagination.
    
    Returns chat messages for the authenticated tenant. Supports filtering and pagination.
    Use the next_cursor from the response to fetch additional pages.
    
    Args:
        limit: Maximum number of messages to return (1-1000, default 100)
        cursor: Optional pagination cursor from previous response
        include_deleted: Whether to include soft-deleted messages (default False)
    
    Returns:
        ChatMessagesListResponse containing items and optional next_cursor
    
    Examples:
        # List first 10 messages
        >>> await list_chat_messages(limit=10)
        
        # List next page
        >>> await list_chat_messages(limit=10, cursor="...")
        
        # Include deleted messages
        >>> await list_chat_messages(include_deleted=True)
    """
    async with get_client() as client:
        params = {"limit": limit}
        if cursor:
            params["cursor"] = cursor
        if include_deleted:
            params["includeDeleted"] = "true"
        
        logger.info(f"Listing chat messages: limit={limit}, cursor={cursor}, include_deleted={include_deleted}")
        response = await call_get(client, "/v1/chat_messages", params=params)
        data = response.json()
        
        return ChatMessagesListResponse(**data)


@mcp.tool()
async def get_chat_message(
    uid: Annotated[str, Field(description="Unique identifier of the chat message")],
    include_deleted: Annotated[bool, Field(description="Allow retrieving deleted messages")] = False,
) -> ChatMessage:
    """
    Retrieve a single chat message by UID.
    
    Args:
        uid: Unique identifier of the chat message (UUID format)
        include_deleted: Whether to allow retrieving soft-deleted messages
    
    Returns:
        ChatMessage object with full details
    
    Raises:
        httpx.HTTPStatusError: 404 if message not found, 410 if deleted (unless include_deleted=True)
    
    Examples:
        # Get a specific message
        >>> await get_chat_message("c1d9b7dc-a1b2-4c3d-9e8f-7a6b5c4d3e2f")
        
        # Get a deleted message
        >>> await get_chat_message("c1d9b7dc-...", include_deleted=True)
    """
    async with get_client() as client:
        params = {}
        if include_deleted:
            params["includeDeleted"] = "true"
        
        logger.info(f"Getting chat message: uid={uid}")
        response = await call_get(client, f"/v1/chat_messages/{uid}", params=params)
        data = response.json()
        
        return ChatMessage(**data)


@mcp.tool()
async def create_chat_message(
    chat_uid: Annotated[str, Field(description="UID of the parent chat")],
    content: Annotated[str, Field(description="Message content")],
    sender: Annotated[Optional[str], Field(description="Sender name or ID")] = None,
    message_type: Annotated[Optional[str], Field(description="Message type (text, system, etc.)")] = None,
    tags: Annotated[Optional[Union[List[str], str]], Field(description="Optional tags for categorization")] = None,
    additional_fields: Annotated[Optional[Union[Dict[str, Any], str]], Field(description="Additional custom fields")] = None,
) -> ChatMessage:
    """
    Create a new chat message.
    
    Chat messages are always linked to a parent chat via chatUid.
    The server automatically generates a UID for the message.
    
    Args:
        chat_uid: UID of the parent chat
        content: Message content (markdown supported)
        sender: Optional sender name or ID
        message_type: Optional message type (text, system, etc.)
        tags: Optional list of tags for categorization
        additional_fields: Optional dictionary of additional custom fields
    
    Returns:
        ChatMessage object with server-generated UID and metadata
    
    Examples:
        # Simple message
        >>> await create_chat_message(
        ...     chat_uid="abc123-...",
        ...     content="Hello team!"
        ... )
        
        # Message with sender
        >>> await create_chat_message(
        ...     chat_uid="abc123-...",
        ...     content="Meeting starts in 5 minutes",
        ...     sender="John Doe",
        ...     message_type="system"
        ... )
        
        # Message with tags
        >>> await create_chat_message(
        ...     chat_uid="abc123-...",
        ...     content="Important update",
        ...     tags=["important", "announcement"]
        ... )
    """
    async with get_client() as client:
        payload: Dict[str, Any] = {
            "chatUid": chat_uid,
            "content": content,
        }
        
        if sender:
            payload["sender"] = sender
        if message_type:
            payload["messageType"] = message_type
        # Parse tags if it\'s a JSON string

        if tags:

            if isinstance(tags, str):

                try:

                    tags = json.loads(tags)

                except json.JSONDecodeError as e:

                    raise ValueError(f"Invalid JSON string for tags: {e}")

            payload["tags"] = tags
        
        # Parse additional_fields if it\'s a JSON string

        
        if additional_fields:

        
            if isinstance(additional_fields, str):

        
                try:

        
                    additional_fields = json.loads(additional_fields)

        
                except json.JSONDecodeError as e:

        
                    raise ValueError(f"Invalid JSON string for additional_fields: {e}")

        
            payload.update(additional_fields)
        
        logger.info(f"Creating chat message: chat_uid={chat_uid}")
        response = await call_post(client, "/v1/chat_messages", json=payload)
        data = response.json()
        
        return ChatMessage(**data)


@mcp.tool()
async def update_chat_message(
    uid: Annotated[str, Field(description="Unique identifier of the chat message")],
    content: Annotated[str, Field(description="Message content")],
    if_match: Annotated[Optional[int], Field(description="Expected version for optimistic locking")] = None,
    additional_fields: Annotated[Optional[Union[Dict[str, Any], str]], Field(description="Additional custom fields")] = None,
) -> ChatMessage:
    """
    Replace a chat message (full update).
    
    Replaces the entire message payload. For partial updates, use patch_chat_message instead.
    Supports optimistic locking via if_match parameter.
    
    Args:
        uid: Unique identifier of the chat message
        content: New message content
        if_match: Expected version number (returns 409 if mismatch)
        additional_fields: Additional custom fields to include
    
    Returns:
        Updated chat message with incremented version
    
    Raises:
        httpx.HTTPStatusError: 409 if version mismatch, 404 if not found
    
    Examples:
        # Simple update
        >>> await update_chat_message("c1d9b7dc-...", content="Updated content")
        
        # Update with optimistic locking
        >>> await update_chat_message("c1d9b7dc-...", content="...", if_match=3)
    """
    async with get_client() as client:
        payload: Dict[str, Any] = {
            "uid": uid,
            "content": content,
        }
        
        # Parse additional_fields if it\'s a JSON string

        
        if additional_fields:

        
            if isinstance(additional_fields, str):

        
                try:

        
                    additional_fields = json.loads(additional_fields)

        
                except json.JSONDecodeError as e:

        
                    raise ValueError(f"Invalid JSON string for additional_fields: {e}")

        
            payload.update(additional_fields)
        
        logger.info(f"Updating chat message: uid={uid}, if_match={if_match}")
        response = await call_put(client, f"/v1/chat_messages/{uid}", json=payload, if_match=if_match)
        data = response.json()
        
        return ChatMessage(**data)


@mcp.tool()
async def patch_chat_message(
    uid: Annotated[str, Field(description="Unique identifier of the chat message")],
    updates: Annotated[Union[Dict[str, Any], str], Field(description="Fields to update (partial)")],
) -> ChatMessage:
    """
    Partially update a chat message.
    
    Only specified fields are updated; other fields remain unchanged.
    Use this for targeted updates when you don't want to replace the entire message.
    
    Args:
        uid: Unique identifier of the chat message
        updates: Dictionary of fields to update (e.g., {"content": "Updated text"})
    
    Returns:
        Updated chat message with incremented version
    
    Examples:
        # Update only content
        >>> await patch_chat_message("c1d9b7dc-...", {"content": "Corrected message"})
        
        # Update multiple fields
        >>> await patch_chat_message("c1d9b7dc-...", {
        ...     "content": "Updated",
        ...     "tags": ["edited"]
        ... })
    """
    async with get_client() as client:
        # Parse updates if it's a JSON string
        if isinstance(updates, str):
            try:
                updates = json.loads(updates)
            except json.JSONDecodeError as e:
                raise ValueError(f"Invalid JSON string for updates: {e}")

        logger.info(f"Patching chat message: uid={uid}, updates={list(updates.keys())}")
        response = await call_patch(client, f"/v1/chat_messages/{uid}", json=updates)
        data = response.json()
        
        return ChatMessage(**data)


@mcp.tool()
async def delete_chat_message(
    uid: Annotated[str, Field(description="Unique identifier of the chat message")],
) -> ChatMessage:
    """
    Soft delete a chat message.
    
    Marks the message as deleted (sets deletedAt timestamp) but doesn't remove it
    from the database. Deleted messages can still be retrieved with include_deleted=True.
    
    Args:
        uid: Unique identifier of the chat message
    
    Returns:
        Deleted chat message with deletedAt timestamp set
    
    Examples:
        >>> await delete_chat_message("c1d9b7dc-a1b2-4c3d-9e8f-7a6b5c4d3e2f")
    """
    async with get_client() as client:
        logger.info(f"Deleting chat message: uid={uid}")
        response = await call_delete(client, f"/v1/chat_messages/{uid}")
        data = response.json()
        
        return ChatMessage(**data)


@mcp.tool()
async def archive_chat_message(
    uid: Annotated[str, Field(description="Unique identifier of the chat message")],
) -> ChatMessage:
    """
    Archive a chat message.
    
    Sets the message's archived flag to true. Archived messages remain accessible
    but are typically hidden from default views.
    
    Args:
        uid: Unique identifier of the chat message
    
    Returns:
        ChatMessage with archived=true
    
    Examples:
        >>> await archive_chat_message("c1d9b7dc-a1b2-4c3d-9e8f-7a6b5c4d3e2f")
    """
    async with get_client() as client:
        logger.info(f"Archiving chat message: uid={uid}")
        response = await call_post(client, f"/v1/chat_messages/{uid}/archive", json={})
        data = response.json()
        
        return ChatMessage(**data)


@mcp.tool()
async def process_chat_message(
    uid: Annotated[str, Field(description="Unique identifier of the chat message")],
    action: Annotated[str, Field(description="Action to perform (mark_read, mark_delivered)")],
    metadata: Annotated[Optional[Union[Dict[str, Any], str]], Field(description="Optional action metadata")] = None,
) -> ChatMessage:
    """
    Process a chat message action (state machine transition).
    
    Supported actions:
    - mark_read: Mark message as read
    - mark_delivered: Mark message as delivered
    
    Args:
        uid: Unique identifier of the chat message
        action: Action to perform
        metadata: Optional metadata for the action
    
    Returns:
        Updated chat message after action is applied
    
    Examples:
        # Mark as read
        >>> await process_chat_message("c1d9b7dc-...", "mark_read")
        
        # Mark as delivered with metadata
        >>> await process_chat_message("c1d9b7dc-...", "mark_delivered", {"timestamp": "2025-11-18T10:00:00Z"})
    """
    async with get_client() as client:
        payload = {"action": action}
        # Parse metadata if it\'s a JSON string
        if metadata:
            if isinstance(metadata, str):
                try:
                    metadata = json.loads(metadata)
                except json.JSONDecodeError as e:
                    raise ValueError(f"Invalid JSON string for metadata: {e}")
            payload["metadata"] = metadata
        
        logger.info(f"Processing chat message: uid={uid}, action={action}")
        response = await call_post(client, f"/v1/chat_messages/{uid}/process", json=payload)
        data = response.json()
        
        return ChatMessage(**data)
