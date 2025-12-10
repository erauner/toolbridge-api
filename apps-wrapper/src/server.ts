/**
 * ChatGPT Apps SDK Wrapper for ToolBridge MCP Server
 *
 * Uses @mcp-ui/server with adapters.appsSdk.enabled for proper ChatGPT integration.
 *
 * Key pattern (from mcpui.dev):
 * - Static templates with adapter enabled → ChatGPT (text/html+skybridge)
 * - Embedded resources without adapter → MCP-UI hosts (text/html)
 * - Tool responses include BOTH for cross-host compatibility
 */

import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import {
  CallToolRequestSchema,
  ListToolsRequestSchema,
  ListResourcesRequestSchema,
  ReadResourceRequestSchema,
} from "@modelcontextprotocol/sdk/types.js";
import { createUIResource, type UIResource } from "@mcp-ui/server";

import { McpClient } from "./mcp-client.js";
import { TOOL_DEFINITIONS } from "./tools/index.js";
import { RESOURCE_DEFINITIONS, createWidgetHtml } from "./resources/index.js";

// Configuration
const PYTHON_MCP_URL = process.env.PYTHON_MCP_URL || "https://toolbridge-mcp-staging.fly.dev/mcp";
const SERVER_NAME = "ToolBridge Apps";
const SERVER_VERSION = "1.0.0";

// Create MCP client to proxy to Python backend
const mcpClient = new McpClient(PYTHON_MCP_URL);

// Create Apps SDK server
const server = new Server(
  {
    name: SERVER_NAME,
    version: SERVER_VERSION,
  },
  {
    capabilities: {
      tools: {},
      resources: {},
    },
  }
);

// ════════════════════════════════════════════════════════════════════════════
// STATIC TEMPLATE RESOURCES - Pre-registered for ChatGPT Apps SDK
// ════════════════════════════════════════════════════════════════════════════

// Create static templates with Apps SDK adapter enabled
// These use text/html+skybridge MIME type and inject the bridge script
const staticTemplates = new Map<string, UIResource>();

for (const resourceDef of RESOURCE_DEFINITIONS) {
  // Ensure URI matches the expected type pattern
  const uri = resourceDef.uri as `ui://${string}`;

  const template = createUIResource({
    uri,
    encoding: "text",
    // Enable Apps SDK adapter for ChatGPT integration
    adapters: {
      appsSdk: {
        enabled: true,
      },
    },
    // Store widget description in metadata
    metadata: {
      widgetDescription: resourceDef.widgetDescription,
    },
    // Initial template content - will be replaced with actual data on tool call
    content: {
      type: "rawHtml",
      htmlString: createWidgetHtml(resourceDef.uri, {}),
    },
  });
  staticTemplates.set(resourceDef.uri, template);
}

// ════════════════════════════════════════════════════════════════════════════
// TOOLS - List tools with Apps SDK metadata
// ════════════════════════════════════════════════════════════════════════════

server.setRequestHandler(ListToolsRequestSchema, async () => {
  console.error("[Apps] Listing tools with Apps SDK metadata");

  return {
    tools: TOOL_DEFINITIONS.map((tool) => ({
      name: tool.name,
      description: tool.description,
      inputSchema: tool.inputSchema,
      // ChatGPT Apps-specific metadata
      _meta: {
        "openai/outputTemplate": tool.outputTemplate,
        "openai/toolInvocation/invoking": tool.invokingMessage || "Loading...",
        "openai/toolInvocation/invoked": tool.invokedMessage || "Ready",
        "openai/widgetAccessible": tool.widgetAccessible ?? true,
      },
    })),
  };
});

// ════════════════════════════════════════════════════════════════════════════
// TOOLS - Call tools by proxying to Python MCP
// ════════════════════════════════════════════════════════════════════════════

server.setRequestHandler(CallToolRequestSchema, async (request) => {
  const { name, arguments: args } = request.params;
  console.error(`[Apps] Calling tool: ${name}`, JSON.stringify(args));

  // Find tool definition for metadata
  const toolDef = TOOL_DEFINITIONS.find((t) => t.name === name);

  try {
    // Proxy to Python MCP backend
    const result = await mcpClient.callTool(name, args || {});

    // Extract structured data from the result if available
    const structuredContent = result.structuredContent || result.content;

    // Create embedded UI resource for MCP-UI hosts (without adapter)
    let embeddedResource: UIResource | null = null;
    if (toolDef?.outputTemplate) {
      const html = createWidgetHtml(toolDef.outputTemplate, structuredContent);
      const embeddedUri = `ui://toolbridge/embedded/${name}` as `ui://${string}`;
      embeddedResource = createUIResource({
        uri: embeddedUri,
        encoding: "text",
        // NO adapter - this is for MCP-UI hosts
        content: {
          type: "rawHtml",
          htmlString: html,
        },
      });
    }

    // Return with both:
    // - _meta["openai/outputTemplate"] for ChatGPT (uses static template resource)
    // - Embedded UI resource for MCP-UI hosts
    return {
      content: [
        // Text fallback for non-widget hosts
        ...(result.content || []),
        // Embedded UI resource for MCP-UI hosts (they ignore _meta)
        ...(embeddedResource ? [embeddedResource] : []),
      ],
      // Structured data for template hydration
      structuredContent,
      // ChatGPT uses this to find the static template resource
      _meta: {
        "openai/outputTemplate": toolDef?.outputTemplate,
      },
    };
  } catch (error) {
    console.error(`[Apps] Tool call failed: ${name}`, error);
    return {
      content: [
        {
          type: "text",
          text: `Error calling ${name}: ${error instanceof Error ? error.message : "Unknown error"}`,
        },
      ],
      isError: true,
    };
  }
});

// ════════════════════════════════════════════════════════════════════════════
// RESOURCES - List HTML widget resources (static templates for ChatGPT)
// ════════════════════════════════════════════════════════════════════════════

server.setRequestHandler(ListResourcesRequestSchema, async () => {
  console.error("[Apps] Listing resources");

  // Return resource metadata for listing
  return {
    resources: RESOURCE_DEFINITIONS.map((def) => ({
      uri: def.uri,
      name: def.name,
      description: def.description,
      mimeType: "text/html+skybridge",
    })),
  };
});

// ════════════════════════════════════════════════════════════════════════════
// RESOURCES - Read HTML content (static templates with adapter)
// ════════════════════════════════════════════════════════════════════════════

server.setRequestHandler(ReadResourceRequestSchema, async (request) => {
  const { uri } = request.params;
  console.error(`[Apps] Reading resource: ${uri}`);

  // Find static template
  const template = staticTemplates.get(uri);

  if (!template) {
    throw new Error(`Resource not found: ${uri}`);
  }

  // Return the template resource content
  return {
    contents: [template.resource],
  };
});

// ════════════════════════════════════════════════════════════════════════════
// START SERVER
// ════════════════════════════════════════════════════════════════════════════

async function main() {
  console.error(`[Apps] Starting ${SERVER_NAME} v${SERVER_VERSION}`);
  console.error(`[Apps] Python MCP backend: ${PYTHON_MCP_URL}`);
  console.error(`[Apps] Registered ${staticTemplates.size} static templates`);

  const transport = new StdioServerTransport();
  await server.connect(transport);

  console.error("[Apps] Server connected via stdio");
}

main().catch((error) => {
  console.error("[Apps] Fatal error:", error);
  process.exit(1);
});
