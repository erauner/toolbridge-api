package tools

// RegisterAllTools registers all available tools with the registry
func RegisterAllTools(r *Registry) {
	// Notes tools
	registerNotesTools(r)

	// Tasks tools
	registerTasksTools(r)

	// Comments tools
	registerCommentsTools(r)

	// Chats tools
	registerChatsTools(r)

	// Chat Messages tools
	registerChatMessagesTools(r)

	// Context attachment tools
	registerContextTools(r)
}

func registerNotesTools(r *Registry) {
	// notes.list
	r.MustRegister(ToolDefinition{
		Name:        "notes.list",
		Description: "List all notes with optional filtering and pagination",
		InputSchema: ListOptsSchema(),
	}, HandleListNotes)

	// notes.get
	r.MustRegister(ToolDefinition{
		Name:        "notes.get",
		Description: "Retrieve a specific note by UID",
		InputSchema: GetOptsSchema(),
	}, HandleGetNote)

	// notes.create
	r.MustRegister(ToolDefinition{
		Name:        "notes.create",
		Description: "Create a new note with the given payload",
		InputSchema: CreateSchema("note"),
	}, HandleCreateNote)

	// notes.update
	r.MustRegister(ToolDefinition{
		Name:        "notes.update",
		Description: "Replace a note's payload entirely (full update with optional optimistic locking)",
		InputSchema: UpdateSchema("note"),
	}, HandleUpdateNote)

	// notes.patch
	r.MustRegister(ToolDefinition{
		Name:        "notes.patch",
		Description: "Partially update specific fields of a note",
		InputSchema: PatchSchema("note"),
	}, HandlePatchNote)

	// notes.delete
	r.MustRegister(ToolDefinition{
		Name:        "notes.delete",
		Description: "Permanently delete a note (soft delete, creates tombstone)",
		InputSchema: DeleteSchema(),
	}, HandleDeleteNote)

	// notes.archive
	r.MustRegister(ToolDefinition{
		Name:        "notes.archive",
		Description: "Archive a note (marks as deleted but retrievable with includeDeleted)",
		InputSchema: ArchiveSchema(),
	}, HandleArchiveNote)

	// notes.process
	r.MustRegister(ToolDefinition{
		Name:        "notes.process",
		Description: "Perform custom actions on a note (pin, unpin, tag, untag, export)",
		InputSchema: ProcessSchema("note", []string{"pin", "unpin", "tag", "untag", "export"}),
	}, HandleProcessNote)
}

func registerTasksTools(r *Registry) {
	// tasks.list
	r.MustRegister(ToolDefinition{
		Name:        "tasks.list",
		Description: "List all tasks with optional filtering and pagination",
		InputSchema: ListOptsSchema(),
	}, HandleListTasks)

	// tasks.get
	r.MustRegister(ToolDefinition{
		Name:        "tasks.get",
		Description: "Retrieve a specific task by UID",
		InputSchema: GetOptsSchema(),
	}, HandleGetTask)

	// tasks.create
	r.MustRegister(ToolDefinition{
		Name:        "tasks.create",
		Description: "Create a new task with the given payload",
		InputSchema: CreateSchema("task"),
	}, HandleCreateTask)

	// tasks.update
	r.MustRegister(ToolDefinition{
		Name:        "tasks.update",
		Description: "Replace a task's payload entirely (full update with optional optimistic locking)",
		InputSchema: UpdateSchema("task"),
	}, HandleUpdateTask)

	// tasks.patch
	r.MustRegister(ToolDefinition{
		Name:        "tasks.patch",
		Description: "Partially update specific fields of a task",
		InputSchema: PatchSchema("task"),
	}, HandlePatchTask)

	// tasks.delete
	r.MustRegister(ToolDefinition{
		Name:        "tasks.delete",
		Description: "Permanently delete a task (soft delete, creates tombstone)",
		InputSchema: DeleteSchema(),
	}, HandleDeleteTask)

	// tasks.archive
	r.MustRegister(ToolDefinition{
		Name:        "tasks.archive",
		Description: "Archive a task (marks as deleted but retrievable with includeDeleted)",
		InputSchema: ArchiveSchema(),
	}, HandleArchiveTask)

	// tasks.process
	r.MustRegister(ToolDefinition{
		Name:        "tasks.process",
		Description: "Perform custom actions on a task (complete, reopen, prioritize, assign)",
		InputSchema: ProcessSchema("task", []string{"complete", "reopen", "prioritize", "assign"}),
	}, HandleProcessTask)
}

func registerCommentsTools(r *Registry) {
	// comments.list
	r.MustRegister(ToolDefinition{
		Name:        "comments.list",
		Description: "List all comments with optional filtering and pagination",
		InputSchema: ListOptsSchema(),
	}, HandleListComments)

	// comments.get
	r.MustRegister(ToolDefinition{
		Name:        "comments.get",
		Description: "Retrieve a specific comment by UID",
		InputSchema: GetOptsSchema(),
	}, HandleGetComment)

	// comments.create
	r.MustRegister(ToolDefinition{
		Name:        "comments.create",
		Description: "Create a new comment on a note or task (requires parentUid and parentKind in payload)",
		InputSchema: CreateSchema("comment"),
	}, HandleCreateComment)

	// comments.update
	r.MustRegister(ToolDefinition{
		Name:        "comments.update",
		Description: "Replace a comment's payload entirely",
		InputSchema: UpdateSchema("comment"),
	}, HandleGenericUpdate("comments"))

	// comments.patch
	r.MustRegister(ToolDefinition{
		Name:        "comments.patch",
		Description: "Partially update specific fields of a comment",
		InputSchema: PatchSchema("comment"),
	}, HandleGenericPatch("comments"))

	// comments.delete
	r.MustRegister(ToolDefinition{
		Name:        "comments.delete",
		Description: "Permanently delete a comment",
		InputSchema: DeleteSchema(),
	}, HandleGenericDelete("comments"))

	// comments.archive
	r.MustRegister(ToolDefinition{
		Name:        "comments.archive",
		Description: "Archive a comment",
		InputSchema: ArchiveSchema(),
	}, HandleGenericArchive("comments"))
}

func registerChatsTools(r *Registry) {
	// chats.list
	r.MustRegister(ToolDefinition{
		Name:        "chats.list",
		Description: "List all chats with optional filtering and pagination",
		InputSchema: ListOptsSchema(),
	}, HandleListChats)

	// chats.get
	r.MustRegister(ToolDefinition{
		Name:        "chats.get",
		Description: "Retrieve a specific chat by UID",
		InputSchema: GetOptsSchema(),
	}, HandleGetChat)

	// chats.create
	r.MustRegister(ToolDefinition{
		Name:        "chats.create",
		Description: "Create a new chat with the given payload",
		InputSchema: CreateSchema("chat"),
	}, HandleCreateChat)

	// chats.update
	r.MustRegister(ToolDefinition{
		Name:        "chats.update",
		Description: "Replace a chat's payload entirely",
		InputSchema: UpdateSchema("chat"),
	}, HandleGenericUpdate("chats"))

	// chats.patch
	r.MustRegister(ToolDefinition{
		Name:        "chats.patch",
		Description: "Partially update specific fields of a chat",
		InputSchema: PatchSchema("chat"),
	}, HandleGenericPatch("chats"))

	// chats.delete
	r.MustRegister(ToolDefinition{
		Name:        "chats.delete",
		Description: "Permanently delete a chat",
		InputSchema: DeleteSchema(),
	}, HandleGenericDelete("chats"))

	// chats.archive
	r.MustRegister(ToolDefinition{
		Name:        "chats.archive",
		Description: "Archive a chat",
		InputSchema: ArchiveSchema(),
	}, HandleGenericArchive("chats"))
}

func registerChatMessagesTools(r *Registry) {
	// chat_messages.list
	r.MustRegister(ToolDefinition{
		Name:        "chat_messages.list",
		Description: "List all chat messages with optional filtering and pagination",
		InputSchema: ListOptsSchema(),
	}, HandleListChatMessages)

	// chat_messages.get
	r.MustRegister(ToolDefinition{
		Name:        "chat_messages.get",
		Description: "Retrieve a specific chat message by UID",
		InputSchema: GetOptsSchema(),
	}, HandleGetChatMessage)

	// chat_messages.create
	r.MustRegister(ToolDefinition{
		Name:        "chat_messages.create",
		Description: "Create a new chat message (requires chatUid in payload)",
		InputSchema: CreateSchema("chat message"),
	}, HandleCreateChatMessage)

	// chat_messages.update
	r.MustRegister(ToolDefinition{
		Name:        "chat_messages.update",
		Description: "Replace a chat message's payload entirely",
		InputSchema: UpdateSchema("chat message"),
	}, HandleGenericUpdate("chat_messages"))

	// chat_messages.patch
	r.MustRegister(ToolDefinition{
		Name:        "chat_messages.patch",
		Description: "Partially update specific fields of a chat message",
		InputSchema: PatchSchema("chat message"),
	}, HandleGenericPatch("chat_messages"))

	// chat_messages.delete
	r.MustRegister(ToolDefinition{
		Name:        "chat_messages.delete",
		Description: "Permanently delete a chat message",
		InputSchema: DeleteSchema(),
	}, HandleGenericDelete("chat_messages"))

	// chat_messages.archive
	r.MustRegister(ToolDefinition{
		Name:        "chat_messages.archive",
		Description: "Archive a chat message",
		InputSchema: ArchiveSchema(),
	}, HandleGenericArchive("chat_messages"))
}

func registerContextTools(r *Registry) {
	// context.attach
	r.MustRegister(ToolDefinition{
		Name:        "context.attach",
		Description: "Attach an entity (note, task, or chat) to the MCP session context (in-memory, non-persistent)",
		InputSchema: BuildSchema(map[string]any{
			"entityUid":  UUIDSchema("UID of the entity to attach"),
			"entityKind": EnumSchema("Type of entity", []string{"note", "task", "chat"}),
			"title":      StringSchema("Optional human-readable title for the attachment"),
		}, []string{"entityUid", "entityKind"}),
	}, HandleAttachContext)

	// context.detach
	r.MustRegister(ToolDefinition{
		Name:        "context.detach",
		Description: "Remove an entity from the MCP session context",
		InputSchema: BuildSchema(map[string]any{
			"entityUid": UUIDSchema("UID of the entity to detach"),
		}, []string{"entityUid"}),
	}, HandleDetachContext)

	// context.list
	r.MustRegister(ToolDefinition{
		Name:        "context.list",
		Description: "List all entities currently attached to the MCP session context",
		InputSchema: BuildSchema(map[string]any{}, nil),
	}, HandleListContext)

	// context.clear
	r.MustRegister(ToolDefinition{
		Name:        "context.clear",
		Description: "Remove all entities from the MCP session context",
		InputSchema: BuildSchema(map[string]any{}, nil),
	}, HandleClearContext)
}
