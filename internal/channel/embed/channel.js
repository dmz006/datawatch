#!/usr/bin/env node
/**
 * datawatch-channel — MCP channel server for Claude Code.
 *
 * Architecture:
 *   datawatch daemon  →  HTTP POST :CHANNEL_PORT/send  →  this server  →  MCP notification  →  Claude Code
 *   Claude Code       →  reply tool call              →  this server  →  HTTP POST :DW_PORT/api/channel/reply
 *
 * Start:
 *   node dist/index.js
 *
 * Env vars:
 *   DATAWATCH_CHANNEL_PORT   HTTP port for receiving from datawatch daemon (default: 7433)
 *   DATAWATCH_API_URL        datawatch API base URL for posting replies (default: http://localhost:8080)
 *   DATAWATCH_TOKEN          bearer token for datawatch API (optional)
 *   CLAUDE_SESSION_ID        session ID to tag in notifications (optional)
 *
 * Register in .mcp.json or CLAUDE.md:
 *   { "mcpServers": { "datawatch": { "command": "node", "args": ["/path/to/channel/dist/index.js"] } } }
 *
 * Launch claude with:
 *   claude --dangerously-load-development-channels ...
 */
import { Server } from '@modelcontextprotocol/sdk/server/index.js';
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js';
import { CallToolRequestSchema, ListToolsRequestSchema, } from '@modelcontextprotocol/sdk/types.js';
import * as http from 'node:http';
const CHANNEL_PORT = parseInt(process.env.DATAWATCH_CHANNEL_PORT ?? '7433', 10);
const DW_API_URL = process.env.DATAWATCH_API_URL ?? 'http://localhost:8080';
const DW_TOKEN = process.env.DATAWATCH_TOKEN ?? '';
const SESSION_ID = process.env.CLAUDE_SESSION_ID ?? '';
// --- MCP server setup -------------------------------------------------------
const mcp = new Server({ name: 'datawatch', version: '0.1.0' }, {
    capabilities: {
        tools: {},
        experimental: {
            'claude/channel': {},
            'claude/channel/permission': {}, // enable permission relay
        },
    },
    instructions: `You are connected to the datawatch monitoring system.
Events arrive as <channel source="datawatch" ...>. Read and act on them.
When you have a response, use the reply tool to send it back.
When you need permission for a tool and permission relay is active,
the request will be forwarded to the user automatically.`,
});
// --- Tools: reply + memory --------------------------------------------------
mcp.setRequestHandler(ListToolsRequestSchema, async () => ({
    tools: [
        {
            name: 'reply',
            description: 'Send a reply message back through the datawatch channel',
            inputSchema: {
                type: 'object',
                properties: {
                    text: {
                        type: 'string',
                        description: 'The reply text to send',
                    },
                    session_id: {
                        type: 'string',
                        description: 'Optional: datawatch session ID to associate the reply with',
                    },
                },
                required: ['text'],
            },
        },
        {
            name: 'memory_remember',
            description: 'Save information to the datawatch memory system',
            inputSchema: {
                type: 'object',
                properties: {
                    content: { type: 'string', description: 'The text to remember' },
                    project_dir: { type: 'string', description: 'Optional project directory for scoping' },
                },
                required: ['content'],
            },
        },
        {
            name: 'memory_recall',
            description: 'Search the datawatch memory system for relevant information',
            inputSchema: {
                type: 'object',
                properties: {
                    query: { type: 'string', description: 'Search query' },
                },
                required: ['query'],
            },
        },
        {
            name: 'memory_list',
            description: 'List recent memories from the datawatch memory system',
            inputSchema: {
                type: 'object',
                properties: {
                    n: { type: 'number', description: 'Number of memories to return (default 50)' },
                },
            },
        },
        {
            name: 'memory_forget',
            description: 'Delete a memory from the datawatch memory system by ID',
            inputSchema: {
                type: 'object',
                properties: {
                    id: { type: 'number', description: 'Memory ID to delete' },
                },
                required: ['id'],
            },
        },
        {
            name: 'memory_stats',
            description: 'Get statistics about the datawatch memory system',
            inputSchema: {
                type: 'object',
                properties: {},
            },
        },
    ],
}));
mcp.setRequestHandler(CallToolRequestSchema, async (req) => {
    if (req.params.name === 'reply') {
        const { text, session_id } = req.params.arguments;
        await postToDatawatch('/api/channel/reply', {
            text,
            session_id: session_id ?? SESSION_ID,
        });
        return { content: [{ type: 'text', text: 'Reply sent.' }] };
    }
    if (req.params.name === 'memory_remember') {
        const { content, project_dir } = req.params.arguments;
        const result = await callParent('/api/memory/save', 'POST', { content, project_dir });
        return { content: [{ type: 'text', text: result }] };
    }
    if (req.params.name === 'memory_recall') {
        const { query } = req.params.arguments;
        const result = await callParent('/api/memory/search?q=' + encodeURIComponent(query), 'GET');
        return { content: [{ type: 'text', text: result }] };
    }
    if (req.params.name === 'memory_list') {
        const { n } = req.params.arguments;
        const result = await callParent('/api/memory/list?n=' + (n || 50), 'GET');
        return { content: [{ type: 'text', text: result }] };
    }
    if (req.params.name === 'memory_forget') {
        const { id } = req.params.arguments;
        const result = await callParent('/api/memory/delete', 'POST', { id });
        return { content: [{ type: 'text', text: result }] };
    }
    if (req.params.name === 'memory_stats') {
        const result = await callParent('/api/memory/stats', 'GET');
        return { content: [{ type: 'text', text: result }] };
    }
    return { content: [{ type: 'text', text: 'Unknown tool.' }] };
});
// --- Permission relay -------------------------------------------------------
// When claude-code requests permission for a tool, forward to datawatch
// so it can ask the user via Signal/Telegram/etc.
// Permission relay: forward claude's tool approval requests to datawatch.
// The MCP SDK doesn't have typed schemas for experimental notifications,
// so we intercept them via the raw transport layer after connection.
// See: https://docs.anthropic.com/en/docs/claude-code/channels-reference#permission-relay
// This is wired up after transport.connect() via a low-level message handler.
// --- HTTP server for receiving messages from datawatch ----------------------
const httpServer = http.createServer((req, res) => {
    if (req.method !== 'POST') {
        res.writeHead(405);
        res.end('Method Not Allowed');
        return;
    }
    let body = '';
    req.on('data', (chunk) => { body += chunk.toString(); });
    req.on('end', async () => {
        try {
            const url = new URL(req.url ?? '/', `http://localhost:${CHANNEL_PORT}`);
            if (url.pathname === '/send') {
                // datawatch → claude: forward message as channel notification
                const { text, source = 'datawatch', session_id = '' } = JSON.parse(body);
                await mcp.notification({
                    method: 'notifications/claude/channel',
                    params: {
                        content: text,
                        meta: { source, session_id },
                    },
                });
                res.writeHead(200, { 'Content-Type': 'application/json' });
                res.end(JSON.stringify({ ok: true }));
            }
            else if (url.pathname === '/permission') {
                // Receive permission verdict from datawatch (user responded yes/no)
                const { request_id, behavior } = JSON.parse(body);
                await mcp.notification({
                    method: 'notifications/claude/channel/permission',
                    params: { request_id, behavior },
                });
                res.writeHead(200, { 'Content-Type': 'application/json' });
                res.end(JSON.stringify({ ok: true }));
            }
            else {
                res.writeHead(404);
                res.end('Not Found');
            }
        }
        catch (e) {
            const msg = e instanceof Error ? e.message : String(e);
            res.writeHead(400, { 'Content-Type': 'application/json' });
            res.end(JSON.stringify({ error: msg }));
        }
    });
});
// When CHANNEL_PORT=0 the OS assigns a free port. Wrap listen in a Promise so
// we can read the actual bound port before notifying the daemon.
const actualPort = await new Promise((resolve, reject) => {
    httpServer.listen(CHANNEL_PORT, '127.0.0.1', () => {
        const addr = httpServer.address();
        resolve(addr?.port ?? CHANNEL_PORT);
    });
    httpServer.once('error', reject);
});
process.stderr.write(`[datawatch-channel] HTTP listener on 127.0.0.1:${actualPort}\n`);
// --- Connect to Claude Code over stdio --------------------------------------
const transport = new StdioServerTransport();
await mcp.connect(transport);
process.stderr.write('[datawatch-channel] MCP channel connected to Claude Code\n');
// Notify datawatch that the channel is ready. datawatch uses this to send the
// session's initial task (if any) as the first channel message.
try {
    await postToDatawatch('/api/channel/ready', { session_id: SESSION_ID, port: actualPort });
}
catch (_) {
    // Best-effort; datawatch may not be running or may not support this endpoint yet.
}
// --- Helpers ----------------------------------------------------------------
// callParent makes an HTTP request to the daemon and returns the response body.
// Used by memory tools that need to return data back to the model.
async function callParent(path, method, body) {
    return new Promise((resolve, reject) => {
        const data = body ? JSON.stringify(body) : null;
        const url = new URL(DW_API_URL + path);
        const opts = {
            hostname: url.hostname,
            port: url.port || '80',
            path: url.pathname + url.search,
            method: method || 'GET',
            headers: {
                ...(data ? { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(data) } : {}),
                ...(DW_TOKEN ? { Authorization: `Bearer ${DW_TOKEN}` } : {}),
            },
        };
        const req = http.request(opts, (res) => {
            let chunks = '';
            res.on('data', (c) => { chunks += c.toString(); });
            res.on('end', () => resolve(chunks));
        });
        req.on('error', reject);
        if (data) req.write(data);
        req.end();
    });
}
async function postToDatawatch(path, body) {
    return new Promise((resolve, reject) => {
        const data = JSON.stringify(body);
        const url = new URL(DW_API_URL + path);
        const opts = {
            hostname: url.hostname,
            port: url.port || '80',
            path: url.pathname + url.search,
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'Content-Length': Buffer.byteLength(data),
                ...(DW_TOKEN ? { Authorization: `Bearer ${DW_TOKEN}` } : {}),
            },
        };
        const req = http.request(opts, (res) => {
            res.resume();
            res.on('end', resolve);
        });
        req.on('error', reject);
        req.write(data);
        req.end();
    });
}
