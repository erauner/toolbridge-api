/**
 * ChatGPT Apps SDK Wrapper for ToolBridge MCP Server
 *
 * Uses @mcp-ui/server with adapters.appsSdk.enabled for proper ChatGPT integration.
 *
 * Key pattern (from mcpui.dev):
 * - Static templates with adapter enabled → ChatGPT (text/html+skybridge)
 * - Embedded resources without adapter → MCP-UI hosts (text/html)
 * - Tool responses include BOTH for cross-host compatibility
 *
 * Supports OAuth 2.1 via WorkOS AuthKit (proxied to Python backend).
 * Supports both stdio (local) and HTTP/SSE (Fly.io) transports.
 */

import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { SSEServerTransport } from "@modelcontextprotocol/sdk/server/sse.js";
import {
  CallToolRequestSchema,
  ListToolsRequestSchema,
  ListResourcesRequestSchema,
  ReadResourceRequestSchema,
} from "@modelcontextprotocol/sdk/types.js";
import { mcpAuthMetadataRouter } from "@modelcontextprotocol/sdk/server/auth/router.js";
import type { OAuthMetadata } from "@modelcontextprotocol/sdk/shared/auth.js";
import { createUIResource, type UIResource } from "@mcp-ui/server";
import express from "express";

import { McpClient } from "./mcp-client.js";
import { TOOL_DEFINITIONS } from "./tools/index.js";
import { RESOURCE_DEFINITIONS, createWidgetHtml } from "./resources/index.js";

// Configuration
const PYTHON_MCP_URL = process.env.PYTHON_MCP_URL || "https://toolbridge-mcp-staging.fly.dev/mcp";
const PYTHON_BASE_URL = PYTHON_MCP_URL.replace(/\/mcp$/, ""); // e.g., https://toolbridge-mcp-staging.fly.dev
const PUBLIC_BASE_URL = process.env.PUBLIC_BASE_URL || "https://toolbridge-apps.fly.dev";
const SERVER_NAME = "ToolBridge Apps";
const SERVER_VERSION = "1.0.0";
const PORT = parseInt(process.env.PORT || "8080", 10);
const USE_HTTP = process.env.USE_HTTP === "true" || process.env.NODE_ENV === "production";

// Cache for OAuth metadata from Python backend
let oauthMetadataCache: OAuthMetadata | null = null;

/**
 * Fetch OAuth metadata from Python backend's well-known endpoint.
 * This tells clients where to authenticate (WorkOS AuthKit).
 */
async function fetchOAuthMetadata(): Promise<OAuthMetadata> {
  if (oauthMetadataCache) {
    return oauthMetadataCache;
  }

  const metadataUrl = `${PYTHON_BASE_URL}/.well-known/oauth-authorization-server`;
  console.error(`[Apps] Fetching OAuth metadata from: ${metadataUrl}`);

  const response = await fetch(metadataUrl);
  if (!response.ok) {
    throw new Error(`Failed to fetch OAuth metadata: ${response.status}`);
  }

  oauthMetadataCache = await response.json() as OAuthMetadata;
  console.error("[Apps] OAuth metadata cached:", {
    issuer: oauthMetadataCache.issuer,
    authorization_endpoint: oauthMetadataCache.authorization_endpoint,
    token_endpoint: oauthMetadataCache.token_endpoint,
  });

  return oauthMetadataCache;
}

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
// START SERVER - HTTP/SSE for Fly.io, stdio for local
// ════════════════════════════════════════════════════════════════════════════

// Store transports by session ID for SSE
const transports = new Map<string, SSEServerTransport>();

// Store access tokens by session ID (set during OAuth flow or from Authorization header)
const sessionTokens = new Map<string, string>();

async function startHttpServer() {
  const app = express();

  // ════════════════════════════════════════════════════════════════════════════
  // OAuth Metadata - Tell clients where to authenticate (WorkOS AuthKit)
  // ════════════════════════════════════════════════════════════════════════════

  // Fetch OAuth metadata from Python backend at startup
  const oauthMetadata = await fetchOAuthMetadata();

  // Mount OAuth metadata router - exposes /.well-known/oauth-authorization-server
  // and /.well-known/oauth-protected-resource/sse
  const resourceServerUrl = new URL(`${PUBLIC_BASE_URL}/sse`);
  app.use(
    mcpAuthMetadataRouter({
      oauthMetadata,
      resourceServerUrl,
      resourceName: SERVER_NAME,
    })
  );

  console.error("[Apps] OAuth metadata router mounted");
  console.error(`[Apps] Authorization server: ${oauthMetadata.issuer}`);
  console.error(`[Apps] Protected resource: ${resourceServerUrl.href}`);

  // ════════════════════════════════════════════════════════════════════════════
  // Health Check
  // ════════════════════════════════════════════════════════════════════════════

  // Health check endpoint
  app.get("/health", (_req, res) => {
    res.json({ status: "ok", server: SERVER_NAME, version: SERVER_VERSION });
  });

  // ════════════════════════════════════════════════════════════════════════════
  // MCP over SSE with OAuth Bearer Token Support
  // ════════════════════════════════════════════════════════════════════════════

  // SSE endpoint for MCP - extract bearer token from Authorization header
  app.get("/sse", async (req, res) => {
    console.error("[Apps] SSE connection request received");

    // Extract bearer token if present
    const authHeader = req.headers.authorization;
    let accessToken: string | undefined;
    if (authHeader?.startsWith("Bearer ")) {
      accessToken = authHeader.substring(7);
      console.error("[Apps] Bearer token received (length:", accessToken.length, ")");
    } else {
      console.error("[Apps] No bearer token in request - client may not be authenticated");
    }

    // Create transport - it tells client to POST to /message?sessionId=xxx
    const transport = new SSEServerTransport("/message", res);
    const sessionId = transport.sessionId;

    console.error(`[Apps] SSE session created: ${sessionId}`);

    // Store transport and token by session ID
    transports.set(sessionId, transport);
    if (accessToken) {
      sessionTokens.set(sessionId, accessToken);
      console.error(`[Apps] Token stored for session: ${sessionId}`);
    }

    // Handle client disconnect
    res.on("close", () => {
      console.error(`[Apps] SSE connection closed: ${sessionId}`);
      transports.delete(sessionId);
      sessionTokens.delete(sessionId);
    });

    // Connect the MCP server to this transport
    await server.connect(transport);

    console.error(`[Apps] SSE connection established: ${sessionId}`);
  });

  // Message endpoint for MCP - receives POST messages from client
  app.post("/message", express.json(), async (req, res) => {
    const sessionId = req.query.sessionId as string;
    console.error(`[Apps] Received message for session ${sessionId}:`, JSON.stringify(req.body));

    const transport = transports.get(sessionId);

    if (!transport) {
      console.error(`[Apps] No transport found for session: ${sessionId}`);
      res.status(400).json({
        jsonrpc: "2.0",
        error: {
          code: -32000,
          message: `No transport found for sessionId: ${sessionId}`,
        },
        id: null,
      });
      return;
    }

    // Check for bearer token in Authorization header (ChatGPT sends it here too)
    const authHeader = req.headers.authorization;
    if (authHeader?.startsWith("Bearer ")) {
      const token = authHeader.substring(7);
      sessionTokens.set(sessionId, token);
      console.error(`[Apps] Token updated for session: ${sessionId}`);
    }

    // Set the token on the MCP client for this request
    const token = sessionTokens.get(sessionId);
    if (token) {
      mcpClient.setAccessToken(token);
    }

    // Pass the message to the transport for processing
    await transport.handlePostMessage(req, res, req.body);
  });

  app.listen(PORT, () => {
    console.error(`[Apps] HTTP server listening on port ${PORT}`);
    console.error(`[Apps] SSE endpoint: http://localhost:${PORT}/sse`);
    console.error(`[Apps] Health check: http://localhost:${PORT}/health`);
  });
}

async function startStdioServer() {
  const transport = new StdioServerTransport();
  await server.connect(transport);
  console.error("[Apps] Server connected via stdio");
}

async function main() {
  console.error(`[Apps] Starting ${SERVER_NAME} v${SERVER_VERSION}`);
  console.error(`[Apps] Python MCP backend: ${PYTHON_MCP_URL}`);
  console.error(`[Apps] Registered ${staticTemplates.size} static templates`);
  console.error(`[Apps] Transport mode: ${USE_HTTP ? "HTTP/SSE" : "stdio"}`);

  if (USE_HTTP) {
    await startHttpServer();
  } else {
    await startStdioServer();
  }
}

main().catch((error) => {
  console.error("[Apps] Fatal error:", error);
  process.exit(1);
});
