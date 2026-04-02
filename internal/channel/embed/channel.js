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
// --- Tools: reply + interrupt -----------------------------------------------
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
// Wait for HTTP server to be listening before connecting MCP.
// This ensures httpServer.address().port is available for the ready callback.
const listenReady = new Promise((resolve) => {
    httpServer.listen(CHANNEL_PORT, '127.0.0.1', () => {
        const actualPort = httpServer.address().port;
        process.stderr.write(`[datawatch-channel] HTTP listener on 127.0.0.1:${actualPort}\n`);
        resolve(actualPort);
    });
});
const channelPort = await listenReady;
// --- Connect to Claude Code over stdio --------------------------------------
const transport = new StdioServerTransport();
await mcp.connect(transport);
process.stderr.write('[datawatch-channel] MCP channel connected to Claude Code\n');
// Notify datawatch that the channel is ready with the actual listening port.
try {
    await postToDatawatch('/api/channel/ready', { session_id: SESSION_ID, port: channelPort });
}
catch (_) {
    // Best-effort; datawatch may not be running or may not support this endpoint yet.
}
// --- Helpers ----------------------------------------------------------------
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
