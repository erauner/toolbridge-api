# Claude Desktop Setup for ToolBridge MCP

This guide shows you how to connect Claude Desktop to your deployed ToolBridge MCP service on Fly.io.

## Prerequisites

- Claude Desktop installed
- ToolBridge MCP service deployed to Fly.io (✅ Complete!)
- Text editor for editing JSON configuration

## Step 1: Locate Claude Desktop Config

The configuration file location depends on your operating system:

**macOS:**
```
~/Library/Application Support/Claude/claude_desktop_config.json
```

**Windows:**
```
%APPDATA%\Claude\claude_desktop_config.json
```

**Linux:**
```
~/.config/Claude/claude_desktop_config.json
```

## Step 2: Edit Configuration

Open the config file in your text editor:

```bash
# macOS
code ~/Library/Application\ Support/Claude/claude_desktop_config.json
# or
nano ~/Library/Application\ Support/Claude/claude_desktop_config.json
```

## Step 3: Add ToolBridge MCP Server

Add the following configuration to connect to your deployed Fly.io service:

```json
{
  "mcpServers": {
    "toolbridge-staging": {
      "url": "https://toolbridge-mcp-staging.fly.dev/sse",
      "transport": "sse",
      "headers": {
        "Authorization": "Bearer YOUR_JWT_TOKEN_HERE"
      }
    }
  }
}
```

### Important Configuration Details

1. **`url`**: Your Fly.io MCP endpoint with `/sse` path
2. **`transport`**: Must be `"sse"` for FastMCP SSE transport
3. **`headers.Authorization`**: JWT token for authentication

### Generating a JWT Token

You need a valid JWT token to authenticate with the Go API. Here are two options:

#### Option A: For Testing (HS256)

If your Go API is using HS256 (development mode), you can generate a token:

```bash
# Create a test token generator script
cat > ~/generate-jwt.py << 'EOF'
import jwt
from datetime import datetime, timedelta

# Configuration
JWT_SECRET = "<JWT_SECRET_FROM_K8S>"  # From K8s secret
USER_ID = "claude-desktop-user"  # Your user ID
TENANT_ID = "staging-tenant-001"

# Generate token
payload = {
    "sub": USER_ID,
    "tenant_id": TENANT_ID,
    "iat": datetime.now(),
    "exp": datetime.now() + timedelta(days=30)  # 30 day expiration
}

token = jwt.encode(payload, JWT_SECRET, algorithm="HS256")
print("\nYour JWT Token:")
print(token)
print("\nAdd this to your Claude Desktop config as:")
print(f'"Authorization": "Bearer {token}"')
EOF

# Run it
python3 ~/generate-jwt.py
```

Copy the generated token and paste it into your Claude Desktop config.

#### Option B: For Production (Auth0)

If your Go API is using Auth0 RS256 tokens (production), you'll need to:

1. Get a token from your Auth0 tenant
2. Use Auth0's authentication flow
3. Or configure Auth0 Machine-to-Machine authentication

For now, testing is easier with Option A.

### Complete Example Configuration

```json
{
  "mcpServers": {
    "toolbridge-staging": {
      "url": "https://toolbridge-mcp-staging.fly.dev/sse",
      "transport": "sse",
      "headers": {
        "Authorization": "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJjbGF1ZGUtZGVza3RvcC11c2VyIiwidGVuYW50X2lkIjoic3RhZ2luZy10ZW5hbnQtMDAxIiwiaWF0IjoxNzAwMDAwMDAwLCJleHAiOjE3MDI1OTIwMDB9.SIGNATURE"
      }
    }
  }
}
```

## Step 4: Restart Claude Desktop

After saving the configuration:

1. **Quit Claude Desktop completely** (not just close the window)
   - macOS: `Cmd+Q` or Claude → Quit Claude
   - Windows: Right-click system tray icon → Exit
   - Linux: Close application

2. **Restart Claude Desktop**

3. **Wait for connection** (may take 5-10 seconds on first start)

## Step 5: Verify Connection

Once Claude Desktop starts, you should see the ToolBridge tools available:

### Check for Tools

In Claude Desktop, you should see indicators for MCP servers. Look for:

- 🔌 **Connected servers** indicator
- 🛠️ **Available tools** count (should show ~40 tools)

### Test with a Simple Prompt

Try asking Claude:

```
List my notes
```

or

```
Create a note titled "Test from Claude Desktop" with content "This is a test"
```

Claude should use the MCP tools to interact with your ToolBridge API.

## Troubleshooting

### "Connection failed" or "Server not responding"

**Check:**
1. Fly.io app is running: `fly status -a toolbridge-mcp-staging`
2. URL is correct: `https://toolbridge-mcp-staging.fly.dev/sse`
3. JWT token is valid (not expired)

**Test endpoint manually:**
```bash
curl -H "Authorization: Bearer YOUR_TOKEN" \
  https://toolbridge-mcp-staging.fly.dev/sse
```

Should return SSE events like:
```
event: endpoint
data: /messages/?session_id=...
```

### "Authentication failed" or 401 errors

**Issue:** Invalid or expired JWT token

**Fix:**
1. Re-generate JWT token with longer expiration
2. Check JWT_SECRET matches your K8s secret
3. Verify token includes required claims (`sub`, `tenant_id`)

**Debug token:**
```bash
# Decode JWT to check contents
python3 -c "import jwt; print(jwt.decode('YOUR_TOKEN', options={'verify_signature': False}))"
```

### "No tools showing up"

**Check:**
1. Claude Desktop config syntax is valid JSON (no trailing commas!)
2. Config file saved in correct location
3. Claude Desktop fully restarted (not just window closed)

**View logs:**
- macOS: `~/Library/Logs/Claude/`
- Windows: `%LOCALAPPDATA%\Claude\logs\`

### "Tools execute but return errors"

**Issue:** MCP can connect but Go API returns errors

**Check:**
1. Go API is healthy: `curl https://toolbridgeapi.erauner.dev/healthz`
2. Tenant headers are correct (secret matches K8s)
3. Fly.io logs for errors: `fly logs -a toolbridge-mcp-staging`

## Advanced Configuration

### Multiple Environments

You can configure multiple MCP servers (dev, staging, prod):

```json
{
  "mcpServers": {
    "toolbridge-dev": {
      "url": "http://localhost:8001/sse",
      "transport": "sse",
      "headers": {
        "Authorization": "Bearer DEV_TOKEN"
      }
    },
    "toolbridge-staging": {
      "url": "https://toolbridge-mcp-staging.fly.dev/sse",
      "transport": "sse",
      "headers": {
        "Authorization": "Bearer STAGING_TOKEN"
      }
    }
  }
}
```

Claude Desktop will connect to all configured servers and show all available tools.

### Custom Timeouts

If you experience timeouts, you can adjust:

```json
{
  "mcpServers": {
    "toolbridge-staging": {
      "url": "https://toolbridge-mcp-staging.fly.dev/sse",
      "transport": "sse",
      "timeout": 30000,  // 30 seconds
      "headers": {
        "Authorization": "Bearer YOUR_TOKEN"
      }
    }
  }
}
```

## Quick Start Script

For convenience, here's a script that sets everything up:

```bash
#!/bin/bash
# setup-claude-desktop.sh

echo "🚀 Setting up Claude Desktop for ToolBridge MCP"

# Generate JWT token
JWT_SECRET="<JWT_SECRET_FROM_K8S>"
USER_ID="claude-desktop-user"
TENANT_ID="staging-tenant-001"

TOKEN=$(python3 - <<EOF
import jwt
from datetime import datetime, timedelta

payload = {
    "sub": "$USER_ID",
    "tenant_id": "$TENANT_ID",
    "iat": datetime.now(),
    "exp": datetime.now() + timedelta(days=30)
}

print(jwt.encode(payload, "$JWT_SECRET", algorithm="HS256"))
EOF
)

# Create config
CONFIG_FILE="$HOME/Library/Application Support/Claude/claude_desktop_config.json"

cat > "$CONFIG_FILE" <<EOFCONFIG
{
  "mcpServers": {
    "toolbridge-staging": {
      "url": "https://toolbridge-mcp-staging.fly.dev/sse",
      "transport": "sse",
      "headers": {
        "Authorization": "Bearer $TOKEN"
      }
    }
  }
}
EOFCONFIG

echo "✅ Configuration saved to: $CONFIG_FILE"
echo ""
echo "🔑 Your JWT token: $TOKEN"
echo ""
echo "📋 Next steps:"
echo "  1. Restart Claude Desktop (Cmd+Q, then reopen)"
echo "  2. Wait 5-10 seconds for connection"
echo "  3. Try: 'List my notes' or 'Create a test note'"
echo ""
echo "🔍 Debug if needed:"
echo "  - Check Fly.io status: fly status -a toolbridge-mcp-staging"
echo "  - View logs: fly logs -a toolbridge-mcp-staging"
echo ""
```

Save as `setup-claude-desktop.sh`, make executable, and run:

```bash
chmod +x setup-claude-desktop.sh
./setup-claude-desktop.sh
```

## Example Usage

Once connected, you can interact with ToolBridge through natural language:

### Notes
```
Create a note titled "Meeting Notes" with content "Discussed Q4 planning"
List all my notes
Show me the note with title "Meeting Notes"
Update my "Meeting Notes" note to add "Action items: Review budget"
Archive the "Meeting Notes" note
```

### Tasks
```
Create a task: "Review pull requests" due tomorrow
List all my open tasks
Mark the "Review pull requests" task as complete
```

### Chats & Messages
```
Create a new chat about "Project Planning"
Add a message to the "Project Planning" chat saying "Let's discuss timeline"
Show me all messages in the "Project Planning" chat
```

## Summary

**Configuration Location:**
```
~/Library/Application Support/Claude/claude_desktop_config.json
```

**Minimal Working Config:**
```json
{
  "mcpServers": {
    "toolbridge-staging": {
      "url": "https://toolbridge-mcp-staging.fly.dev/sse",
      "transport": "sse",
      "headers": {
        "Authorization": "Bearer YOUR_JWT_TOKEN"
      }
    }
  }
}
```

**After Configuration:**
1. Save config file
2. Restart Claude Desktop (full quit + reopen)
3. Test with: "List my notes"

**Need Help?**
- Check Fly.io logs: `fly logs -a toolbridge-mcp-staging`
- Verify endpoint: `curl https://toolbridge-mcp-staging.fly.dev/sse`
- View Claude Desktop logs: `~/Library/Logs/Claude/`
