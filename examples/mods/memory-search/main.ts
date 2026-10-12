// An agent tool (mods design §2, tier 5): memory-search_find searches the
// calling session's project memory for text and answers the lines that
// hold it. The server checks the agent's input against the manifest's
// schema before the handler sees it, and runs the call only for a
// session of a project the mod runs for. What the handler returns is the
// agent's text; a throw is the tool's error, its message what the agent
// reads.
//
// The input is data from an agent, never instructions: the handler only
// matches it as text. It reads with project_read, as any mod may; the
// agent gets nothing it couldn't read with tm itself, in one call.
import type { TM } from "./types/tm";
declare const $: TM;

const LIMIT = 20;
const MAX_LINE = 200;

$.tool.handle("find", async (input, call) => {
  const text = String(input.text ?? "").toLowerCase();
  const limit = typeof input.limit === "number" ? input.limit : LIMIT;
  if (text.trim().length < 2) throw new Error("text needs at least two characters");
  const files = await $.memory.list(call.project);
  const out: string[] = [];
  let more = 0;
  for (const f of files) {
    const lines = (await $.memory.read(call.project, f.path)).split("\n");
    lines.forEach((line, i) => {
      if (!line.toLowerCase().includes(text)) return;
      if (out.length >= limit) more++;
      else out.push(`${f.path}:${i + 1}: ${line.length > MAX_LINE ? line.slice(0, MAX_LINE) + "…" : line}`);
    });
  }
  if (out.length === 0) return `No line of ${call.project}'s memory holds "${input.text}".`;
  if (more > 0) out.push(`(${more} more; ask with a higher limit or narrower text)`);
  return out.join("\n");
});
