package tools

// Common JSON Schema building blocks

// StringSchema creates a JSON schema for a string field
func StringSchema(description string) map[string]any {
	return map[string]any{
		"type":        "string",
		"description": description,
	}
}

// UUIDSchema creates a JSON schema for a UUID field
func UUIDSchema(description string) map[string]any {
	return map[string]any{
		"type":        "string",
		"format":      "uuid",
		"description": description,
	}
}

// IntegerSchema creates a JSON schema for an integer field with optional min/max
func IntegerSchema(description string, min, max *int) map[string]any {
	schema := map[string]any{
		"type":        "integer",
		"description": description,
	}
	if min != nil {
		schema["minimum"] = *min
	}
	if max != nil {
		schema["maximum"] = *max
	}
	return schema
}

// BooleanSchema creates a JSON schema for a boolean field
func BooleanSchema(description string) map[string]any {
	return map[string]any{
		"type":        "boolean",
		"description": description,
	}
}

// ObjectSchema creates a JSON schema for an object with arbitrary properties
func ObjectSchema(description string) map[string]any {
	return map[string]any{
		"type":        "object",
		"description": description,
	}
}

// EnumSchema creates a JSON schema for an enum field
func EnumSchema(description string, values []string) map[string]any {
	return map[string]any{
		"type":        "string",
		"description": description,
		"enum":        values,
	}
}

// ArraySchema creates a JSON schema for an array field
func ArraySchema(description string, items map[string]any) map[string]any {
	return map[string]any{
		"type":        "array",
		"description": description,
		"items":       items,
	}
}

// BuildSchema creates a complete JSON schema object with properties and required fields
func BuildSchema(properties map[string]any, required []string) map[string]any {
	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// ListOptsSchema returns the common schema for list operation parameters
func ListOptsSchema() map[string]any {
	min1, max1000 := 1, 1000
	return BuildSchema(map[string]any{
		"cursor": StringSchema("Pagination cursor from previous response"),
		"limit":  IntegerSchema("Maximum number of items to return (1-1000)", &min1, &max1000),
		"includeDeleted": BooleanSchema("Include soft-deleted items in results"),
	}, nil)
}

// GetOptsSchema returns the schema for get operation parameters
func GetOptsSchema() map[string]any {
	return BuildSchema(map[string]any{
		"uid":            UUIDSchema("Unique identifier of the entity"),
		"includeDeleted": BooleanSchema("Allow retrieving soft-deleted items"),
	}, []string{"uid"})
}

// CreateSchema returns the schema for create operations
func CreateSchema(entityDesc string) map[string]any {
	return BuildSchema(map[string]any{
		"uid":     UUIDSchema("Optional client-generated UUID (server generates if omitted)"),
		"payload": ObjectSchema("Entity data as key-value pairs for " + entityDesc),
	}, []string{"payload"})
}

// UpdateSchema returns the schema for update operations (full replacement)
func UpdateSchema(entityDesc string) map[string]any {
	return BuildSchema(map[string]any{
		"uid":     UUIDSchema("Unique identifier of the entity to update"),
		"payload": ObjectSchema("Complete replacement payload for " + entityDesc),
		"version": IntegerSchema("Expected version for optimistic locking (optional)", nil, nil),
	}, []string{"uid", "payload"})
}

// PatchSchema returns the schema for patch operations (partial update)
func PatchSchema(entityDesc string) map[string]any {
	return BuildSchema(map[string]any{
		"uid":     UUIDSchema("Unique identifier of the entity to patch"),
		"partial": ObjectSchema("Partial update payload with fields to modify for " + entityDesc),
	}, []string{"uid", "partial"})
}

// DeleteSchema returns the schema for delete operations
func DeleteSchema() map[string]any {
	return BuildSchema(map[string]any{
		"uid": UUIDSchema("Unique identifier of the entity to delete"),
	}, []string{"uid"})
}

// ArchiveSchema returns the schema for archive operations (soft delete)
func ArchiveSchema() map[string]any {
	return BuildSchema(map[string]any{
		"uid": UUIDSchema("Unique identifier of the entity to archive"),
	}, []string{"uid"})
}

// ProcessSchema returns the schema for process operations with allowed actions
func ProcessSchema(entityDesc string, allowedActions []string) map[string]any {
	return BuildSchema(map[string]any{
		"uid":      UUIDSchema("Unique identifier of the entity to process"),
		"action":   EnumSchema("Action to perform on the "+entityDesc, allowedActions),
		"metadata": ObjectSchema("Optional metadata for the action"),
	}, []string{"uid", "action"})
}
