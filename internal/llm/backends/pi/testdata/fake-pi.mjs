// A stand-in for pi: it loads the datawatch extension and drives the events pi
// would raise. One JSON command per input line, one JSON answer per command.
import { createInterface } from "node:readline";
import { pathToFileURL } from "node:url";

const handlers = {};
const tools = {};
const sent = []; // {text, deliverAs}
const history = []; // user messages in the conversation
const pi = {
	on: (name, fn) => (handlers[name] ??= []).push(fn),
	registerTool: (t) => (tools[t.name] = t),
	sendUserMessage: (text, opts) => {
		sent.push({ text, deliverAs: opts?.deliverAs ?? "" });
		history.push(text);
	},
};
const emit = async (name, event) => {
	for (const fn of handlers[name] ?? []) await fn(event);
};
const extension = await import(pathToFileURL(process.argv[2]).href);
extension.default(pi);

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
for await (const line of createInterface({ input: process.stdin })) {
	const cmd = JSON.parse(line);
	const out = {};
	try {
		if (cmd.cmd === "start") await emit("session_start", {});
		if (cmd.cmd === "stop") await emit("session_shutdown", {});
		if (cmd.cmd === "sleep") await sleep(cmd.ms);
		if (cmd.cmd === "agent_start") {
			const sections = {};
			await emit("before_agent_start", { systemPromptOptions: { sections } });
			out.sections = sections;
		}
		if (cmd.cmd === "model") {
			// One model call. strip: the injected text never reaches the model.
			const users = cmd.strip ? ["something else"] : history;
			const content = (t) => (cmd.parts ? [{ type: "text", text: t }] : t);
			const messages = users.map((t) => ({ role: "user", content: content(t) }));
			if (cmd.otherRole) messages.forEach((m) => (m.role = cmd.otherRole));
			await emit("before_provider_request", { payload: { messages } });
			if (!cmd.noResponse) await emit("after_provider_response", { status: cmd.status ?? 200 });
			await emit("message_end", { message: { role: "assistant", stopReason: cmd.stopReason ?? "stop" } });
		}
		if (cmd.cmd === "tool") {
			try {
				const res = await tools[cmd.name].execute("call-1", cmd.params);
				out.ok = true;
				out.result = res?.content?.[0]?.text ?? "";
			} catch (e) {
				out.toolError = String(e.message ?? e);
			}
		}
	} catch (e) {
		out.error = String(e.stack ?? e);
	}
	process.stdout.write(JSON.stringify({ ...out, sent, tools: Object.keys(tools) }) + "\n");
}
process.exit(0);
