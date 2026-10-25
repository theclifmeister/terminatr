// What the pane shows and runs, without Obsidian: tm needs --json's
// shape, the tm command behind each button, and the stable-row rule
// (docs/SPEC.md §5.1, Vault: the Obsidian plugin). The plugin decides
// nothing: tm needs says what waits on the user and which actions each
// takes; tm's caller rules decide whether a command may run.

// SCHEMA is the tm needs --json schema this plugin reads. An older tm
// (a lower schema, or no tm needs at all) is told to update; a newer
// schema asks for a newer plugin (tm vault init --plugin).
export const SCHEMA = 1;

// STABLE_MS is how long a row must be unchanged before a click acts on
// it: the console's rule for mods' bound actions.
export const STABLE_MS = 300;

export interface QuestionOption {
	label: string;
	description?: string;
}

export interface Question {
	id: string;
	task?: string;
	header: string;
	question: string;
	options: QuestionOption[];
	multi_select?: boolean;
	recommended?: string;
}

export interface FileOffer {
	id: string;
	name: string;
	size: number;
}

// Need is one entry of tm needs --json. Summary is tm's fixed words;
// title, reason, question and a file's name are other parties' words:
// data, shown framed and never passed to a command.
export interface Need {
	kind: string;
	project?: string;
	server?: string;
	id?: string;
	peer?: string;
	summary: string;
	status?: string;
	title?: string;
	thread?: string;
	reason?: string;
	asked?: string;
	question?: Question;
	file?: FileOffer;
	actions: string[];
}

export interface NeedsList {
	schema: number;
	version: string;
	running: boolean;
	needs: Need[];
}

// Parsed is tm needs --json read: the list, or why it can't be shown.
export type Parsed = { ok: true; list: NeedsList } | { ok: false; error: string };

// parseNeeds reads tm needs --json's output. A schema other than SCHEMA
// is refused with what to update.
export function parseNeeds(out: string): Parsed {
	let v: unknown;
	try {
		v = JSON.parse(out);
	} catch {
		return { ok: false, error: "tm needs --json printed something that isn't JSON: update tm" };
	}
	if (typeof v !== "object" || v === null || Array.isArray(v)) {
		return { ok: false, error: "tm needs --json printed no object: update tm" };
	}
	const o = v as Record<string, unknown>;
	const schema = typeof o.schema === "number" ? o.schema : 0;
	if (schema < SCHEMA) {
		return { ok: false, error: `this tm is older than the plugin (tm needs --json schema ${schema}, the plugin reads ${SCHEMA}): update tm` };
	}
	if (schema > SCHEMA) {
		return { ok: false, error: `this tm is newer than the plugin (tm needs --json schema ${schema}, the plugin reads ${SCHEMA}): tm vault init --plugin updates it` };
	}
	if (!Array.isArray(o.needs)) {
		return { ok: false, error: "tm needs --json has no needs list: update tm" };
	}
	const needs: Need[] = [];
	for (const n of o.needs) {
		if (typeof n !== "object" || n === null || typeof (n as Need).kind !== "string" || typeof (n as Need).summary !== "string") {
			continue;
		}
		const need = n as Need;
		need.actions = Array.isArray(need.actions) ? need.actions.filter((a) => typeof a === "string") : [];
		needs.push(need);
	}
	return { ok: true, list: { schema, version: String(o.version ?? ""), running: o.running === true, needs } };
}

// rowKey names a row across refreshes: its kind, where it is and its id.
export function rowKey(n: Need): string {
	return [n.kind, n.server ?? "", n.project ?? "", n.peer ?? "", n.id ?? ""].join("\u0000");
}

// fingerprint is everything a row shows and acts on: a row whose
// fingerprint changed is a different row to click.
export function fingerprint(n: Need): string {
	return JSON.stringify(n);
}

// Rows tracks when each row last changed, for the stable-row rule.
export class Rows {
	private seen = new Map<string, { print: string; since: number }>();

	// update records list's rows at time now: a row new or changed since
	// the last update is changed now; rows gone are forgotten.
	update(needs: Need[], now: number): void {
		const next = new Map<string, { print: string; since: number }>();
		for (const n of needs) {
			const key = rowKey(n);
			const print = fingerprint(n);
			const was = this.seen.get(key);
			next.set(key, was && was.print === print ? was : { print, since: now });
		}
		this.seen = next;
	}

	// check says why a click at time now on row n, as it was drawn,
	// can't act: gone, changed since it was drawn, or changed in the
	// last STABLE_MS. "" when it may.
	check(n: Need, now: number): string {
		const was = this.seen.get(rowKey(n));
		if (!was) {
			return "that row is gone: nothing ran";
		}
		if (was.print !== fingerprint(n)) {
			return "that row changed since it was drawn: look again; nothing ran";
		}
		if (now - was.since < STABLE_MS) {
			return "that row just changed: look again; nothing ran";
		}
		return "";
	}
}

// Input is what the user typed into the pane's own boxes: the only text
// that reaches a command besides tm's ids.
export interface Input {
	note?: string; // a send-back's note
	choices?: string[]; // a question's chosen options, by number from 1
	other?: string; // a question's Other text
}

// ID_RE is what an id from tm needs looks like (T12, Q3, M-18, R-…, S-…,
// a project slug): never a flag; a value that isn't is never passed to
// tm. Typed text goes as --note=… and --other=…, so it can't be a flag
// either.
const ID_RE = /^[A-Za-z0-9][A-Za-z0-9._@\/-]{0,127}$/;

// CONSOLE_ONLY are the actions tm needs lists that the pane leaves to
// the console, by "kind action": accepting a link is a consent step, as
// accepting a file or a share is.
const CONSOLE_ONLY = new Set(["link-request accept"]);

// buttons are the pane's buttons for n: its actions, with those only the
// console takes as one In the console button.
export function buttons(n: Need): string[] {
	const out: string[] = [];
	for (const a of n.actions) {
		const b = CONSOLE_ONLY.has(`${n.kind} ${a}`) ? "console" : a;
		if (!out.includes(b)) {
			out.push(b);
		}
	}
	return out;
}

// command is the tm command the button for action runs on need n, as
// argv after tm; null for an action that only the console takes or a
// need it doesn't fit. Every one is a human-only tm command, the same
// call the console makes; none passes --approved-by-user, changes a
// setting, pairs or shares.
export function command(n: Need, action: string, input: Input = {}): string[] | null {
	if (!n.actions.includes(action) || CONSOLE_ONLY.has(`${n.kind} ${action}`)) {
		return null;
	}
	const at = (args: string[]): string[] | null => {
		if (!n.project || !ID_RE.test(n.project)) {
			return null;
		}
		return [...args, "--project", n.project];
	};
	const id = n.id && ID_RE.test(n.id) ? n.id : "";
	if (!id) {
		return null;
	}
	switch (`${n.kind} ${action}`) {
		case "task accept":
			return at(["task", "accept", id]);
		case "task send-back": {
			const note = (input.note ?? "").trim();
			return note ? at(["task", "send-back", id, `--note=${note}`]) : null;
		}
		case "task delegate":
			return at(["task", "delegate", id]);
		case "question answer": {
			const args = ["ask", "answer", id];
			const opts = n.question?.options.length ?? 0;
			const choices = input.choices ?? [];
			for (const c of choices) {
				const k = Number(c);
				if (!Number.isInteger(k) || k < 1 || k > opts) {
					return null;
				}
				args.push("--choice", String(k));
			}
			const other = (input.other ?? "").trim();
			if (other) {
				args.push(`--other=${other}`);
			}
			if (choices.length === 0 && !other) {
				return null;
			}
			if (choices.length > 1 && !n.question?.multi_select) {
				return null;
			}
			return at(args);
		}
		case "link-held approve":
			return at(["link", "approve", id]);
		case "link-held drop":
			return at(["link", "drop", id]);
		case "link-request decline":
			return at(["link", "decline", id]);
		case "link-paused resume":
			return at(["link", "resume", id]);
		case "share-request decline":
			return ["peer", "decline", id];
	}
	return null;
}

// LABELS are the buttons' words, by action.
export const LABELS: Record<string, string> = {
	accept: "Accept",
	"send-back": "Send back…",
	delegate: "Delegate",
	answer: "Answer…",
	approve: "Send",
	drop: "Drop",
	decline: "Decline",
	resume: "Resume",
	console: "In the console",
};

// heading is a row's first line in tm's words: where it is and what
// kind of need it is.
export function heading(n: Need): string {
	const where = n.server ? (n.project ? `${n.server} › ${n.project}` : n.server) : n.project || "outside the projects";
	const kind: Record<string, string> = {
		session: "session",
		question: "question",
		"link-held": "held message",
		"link-file": "file offered",
		"link-request": "link request",
		"link-paused": "paused link",
		task: n.status === "review" ? "task in review" : "task",
		"share-request": "share request",
		remote: "paired server",
	};
	return `${where} · ${kind[n.kind] ?? n.kind}`;
}

// consoleHint is what the In the console button says: where in tm the
// user answers n.
export function consoleHint(n: Need): string {
	switch (n.kind) {
		case "link-file":
			return "Files offered are a consent setting: answer it in tm's Network view (N).";
		case "share-request":
			return "Accepting shares a project: do it in tm's Network view (N).";
		case "link-request":
			return "Accepting a link is a consent step: do it in tm's Network view (N).";
		case "remote":
			return "A paired server's need: open it in tm's Network view (N).";
		case "session":
			return "Answer the blocked session in tm, on its pane.";
	}
	return "Answer it in tm, the console.";
}

// notePath is the vault note a row's Open or Read button shows, by the
// paths tm renders: a task's note, or (found by name under the
// project's messages/) a message's data note. null when there is none.
export function notePath(n: Need, files: string[]): string | null {
	if (!n.project || !n.id || !ID_RE.test(n.project) || !ID_RE.test(n.id)) {
		return null;
	}
	if (n.kind === "task") {
		const p = `${n.project}/tasks/${n.id}.md`;
		return files.includes(p) ? p : null;
	}
	if (n.kind === "link-held") {
		const prefix = `${n.project}/messages/`;
		return files.find((f) => f.startsWith(prefix) && f.endsWith(`/${n.id}.md`)) ?? null;
	}
	return null;
}
