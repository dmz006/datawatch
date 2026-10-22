// Throwaway fake Ollama for the model-pin smoke run (tmp/verify/model-pin-smoke.mjs).
//
// Serves GET /api/ps (the loaded-models list the test controls), GET /api/tags
// (for the compute node save probe), and POST /api/generate, which records the
// model and keep_alive of every call and, when load_on_generate is set, loads
// the requested model into /api/ps the way Ollama does.
//
// Control endpoints (test only):
//   GET  /_ctl/requests            every /api/generate call recorded so far
//   POST /_ctl/requests/clear      forget recorded calls
//   POST /_ctl/loaded              replace the loaded list: {"models":[{"name":..,"expires_at":..}]}
//   POST /_ctl/load_on_generate    {"enabled": bool}

import http from "node:http";

const port = Number(process.env.PORT || 0);
const zeroTime = "0001-01-01T00:00:00Z"; // Go's zero time: reads as keep_alive -1 (unlimited)

let loaded = new Map(); // name -> {name, model, expires_at}
let requests = [];
let loadOnGenerate = true;

function readBody(req) {
  return new Promise((resolve) => {
    let data = "";
    req.on("data", (c) => (data += c));
    req.on("end", () => resolve(data));
  });
}

function sendJSON(res, code, obj) {
  res.writeHead(code, { "Content-Type": "application/json" });
  res.end(JSON.stringify(obj));
}

const server = http.createServer(async (req, res) => {
  const body = req.method === "POST" ? await readBody(req) : "";
  const url = req.url.split("?")[0];

  if (req.method === "GET" && url === "/api/ps") {
    return sendJSON(res, 200, { models: [...loaded.values()] });
  }
  if (req.method === "GET" && url === "/api/tags") {
    const names = [...new Set([...requests.map((r) => r.model)])];
    return sendJSON(res, 200, { models: names.map((name) => ({ name, model: name })) });
  }
  if (req.method === "POST" && url === "/api/generate") {
    let parsed = {};
    try {
      parsed = JSON.parse(body || "{}");
    } catch {
      return sendJSON(res, 400, { error: "bad json" });
    }
    const keepAlive = parsed.keep_alive === undefined ? null : parsed.keep_alive;
    requests.push({
      at: new Date().toISOString(),
      model: parsed.model || "",
      keep_alive: keepAlive,
      prompt: parsed.prompt ?? "",
      stream: parsed.stream ?? null,
    });
    if (loadOnGenerate && parsed.model) {
      const expires = keepAlive === -1 ? zeroTime : new Date(Date.now() + 5 * 60 * 1000).toISOString();
      loaded.set(parsed.model, { name: parsed.model, model: parsed.model, expires_at: expires });
    }
    return sendJSON(res, 200, { model: parsed.model, response: "fake answer", done: true });
  }
  if (req.method === "GET" && url === "/_ctl/requests") {
    return sendJSON(res, 200, { requests });
  }
  if (req.method === "POST" && url === "/_ctl/requests/clear") {
    requests = [];
    return sendJSON(res, 200, { ok: true });
  }
  if (req.method === "POST" && url === "/_ctl/loaded") {
    const parsed = JSON.parse(body || "{}");
    loaded = new Map();
    for (const m of parsed.models || []) {
      loaded.set(m.name, { name: m.name, model: m.name, expires_at: m.expires_at || zeroTime });
    }
    return sendJSON(res, 200, { ok: true, loaded: [...loaded.keys()] });
  }
  if (req.method === "POST" && url === "/_ctl/load_on_generate") {
    loadOnGenerate = Boolean(JSON.parse(body || "{}").enabled);
    return sendJSON(res, 200, { ok: true, load_on_generate: loadOnGenerate });
  }
  sendJSON(res, 404, { error: `no route ${req.method} ${url}` });
});

server.listen(port, "127.0.0.1", () => {
  const addr = server.address();
  console.log(JSON.stringify({ fake_ollama_listening: `http://127.0.0.1:${addr.port}` }));
});
