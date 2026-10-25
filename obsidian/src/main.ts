// terminatr, the Obsidian plugin (docs/SPEC.md §5.1, Vault: the
// Obsidian plugin): one side pane, NEEDS YOU across the projects, with
// the console's buttons. A thin front end: tm needs --json says what
// waits and which actions each takes; a click runs the human's own tm
// command, as the user, and tm's caller rules decide. It stores nothing
// but where tm is, changes no tm setting, never pairs or shares, and
// shows other parties' words as text, framed.

import { execFile } from "child_process";
import { existsSync } from "fs";
import { homedir } from "os";
import { delimiter, join } from "path";
import { App, ItemView, Modal, Notice, Plugin, PluginSettingTab, Setting, WorkspaceLeaf, debounce } from "obsidian";
import { LABELS, Need, NeedsList, Rows, command, consoleHint, heading, notePath, parseNeeds } from "./needs";

const VIEW = "terminatr-needs";

// REFRESH_MS is the pane's refresh besides the vault's file events.
const REFRESH_MS = 30_000;

// NOTE_MAX is the longest send-back note tm takes (project.MaxSendBackNote).
const NOTE_MAX = 200;

interface Settings {
	// tmPath is the tm binary: tm vault init --plugin writes its own;
	// empty looks on PATH and in the usual places.
	tmPath: string;
}

const DEFAULTS: Settings = { tmPath: "" };

interface Ran {
	code: number;
	stdout: string;
	stderr: string;
}

export default class TerminatrPlugin extends Plugin {
	settings: Settings = { ...DEFAULTS };

	async onload(): Promise<void> {
		this.settings = { ...DEFAULTS, ...((await this.loadData()) as Partial<Settings> | null) };
		this.registerView(VIEW, (leaf) => new NeedsView(leaf, this));
		this.addRibbonIcon("alert-circle", "terminatr: needs you", () => void this.open());
		this.addCommand({ id: "open-needs-you", name: "Open NEEDS YOU", callback: () => void this.open() });
		this.addSettingTab(new TerminatrSettings(this.app, this));
	}

	async open(): Promise<void> {
		const leaves = this.app.workspace.getLeavesOfType(VIEW);
		let leaf: WorkspaceLeaf | null = leaves[0] ?? null;
		if (!leaf) {
			leaf = this.app.workspace.getRightLeaf(false);
			if (!leaf) {
				return;
			}
			await leaf.setViewState({ type: VIEW, active: true });
		}
		this.app.workspace.revealLeaf(leaf);
	}

	// tm is the binary to run: the setting, else tm on PATH or in the
	// usual install folders (an app started from the Dock has a short
	// PATH).
	tm(): string {
		if (this.settings.tmPath) {
			return this.settings.tmPath;
		}
		const name = process.platform === "win32" ? "tm.exe" : "tm";
		const dirs = (process.env.PATH ?? "").split(delimiter).filter((d) => d !== "");
		dirs.push("/opt/homebrew/bin", "/usr/local/bin", join(homedir(), ".local", "bin"), join(homedir(), "go", "bin"));
		for (const d of dirs) {
			const p = join(d, name);
			if (existsSync(p)) {
				return p;
			}
		}
		return name;
	}

	// run runs tm with args as the user, no shell: argv as given.
	run(args: string[], timeout: number): Promise<Ran> {
		return new Promise((resolve) => {
			execFile(this.tm(), args, { timeout, maxBuffer: 4 << 20, windowsHide: true }, (err, stdout, stderr) => {
				let code = 0;
				if (err) {
					const c = (err as NodeJS.ErrnoException).code;
					code = typeof c === "number" ? c : 1;
					if (c === "ENOENT") {
						stderr = `tm isn't at ${this.tm()}: set where it is in the plugin's settings`;
					}
				}
				resolve({ code, stdout: String(stdout), stderr: String(stderr) });
			});
		});
	}
}

class NeedsView extends ItemView {
	private rows = new Rows();
	private list: NeedsList | null = null;
	private error = "";
	private busy = false;
	private loading = false;
	private last = "";

	constructor(
		leaf: WorkspaceLeaf,
		private plugin: TerminatrPlugin,
	) {
		super(leaf);
	}

	getViewType(): string {
		return VIEW;
	}

	getDisplayText(): string {
		return "NEEDS YOU";
	}

	getIcon(): string {
		return "alert-circle";
	}

	async onOpen(): Promise<void> {
		const soon = debounce(() => void this.refresh(), 500, true);
		this.registerEvent(this.app.vault.on("create", soon));
		this.registerEvent(this.app.vault.on("modify", soon));
		this.registerEvent(this.app.vault.on("delete", soon));
		this.registerInterval(window.setInterval(() => void this.refresh(), REFRESH_MS));
		this.draw();
		await this.refresh();
	}

	async refresh(): Promise<void> {
		if (this.loading) {
			return;
		}
		this.loading = true;
		try {
			const r = await this.plugin.run(["needs", "--json"], 15_000);
			if (r.code !== 0) {
				this.error = firstLine(r.stderr) || `tm needs exited ${r.code}: update tm`;
			} else {
				const p = parseNeeds(r.stdout);
				if (p.ok) {
					this.list = p.list;
					this.error = "";
					this.rows.update(p.list.needs, Date.now());
				} else {
					this.error = p.error;
				}
			}
		} finally {
			this.loading = false;
		}
		this.draw();
	}

	private draw(): void {
		const root = this.contentEl;
		root.empty();
		root.addClass("tm-needs");
		const head = root.createDiv({ cls: "tm-needs-head" });
		const n = this.list?.needs.length ?? 0;
		head.createSpan({ cls: "tm-needs-title", text: n === 0 ? "nothing needs you" : `needs you · ${n}` });
		const again = head.createEl("button", { text: "Refresh" });
		again.onclick = () => void this.refresh();
		if (this.error) {
			root.createDiv({ cls: "tm-needs-error", text: this.error });
		}
		if (this.list && !this.list.running) {
			root.createDiv({ cls: "tm-needs-note", text: "the server isn't running: blocked sessions and the paired servers' needs aren't known" });
		}
		const files = this.app.vault.getMarkdownFiles().map((f) => f.path);
		for (const need of this.list?.needs ?? []) {
			this.drawNeed(root.createDiv({ cls: "tm-need" }), need, files);
		}
		root.createDiv({ cls: "tm-needs-foot", text: this.last ? `ran: tm ${this.last}` : "Each button runs your tm, as you." });
	}

	private drawNeed(el: HTMLElement, need: Need, files: string[]): void {
		el.createDiv({ cls: "tm-need-heading", text: heading(need) });
		el.createDiv({ cls: "tm-need-summary", text: need.summary });
		if (need.title) {
			data(el, need.title);
		}
		if (need.reason) {
			data(el, need.reason);
		}
		if (need.question) {
			data(el, need.question.question);
		}
		if (need.file) {
			data(el, need.file.name);
		}
		const bar = el.createDiv({ cls: "tm-need-buttons" });
		for (const action of need.actions) {
			const b = bar.createEl("button", { text: LABELS[action] ?? action });
			if (action === "accept" || action === "approve") {
				b.addClass("mod-cta");
			}
			b.disabled = this.busy;
			b.onclick = () => this.click(need, action);
		}
		const note = notePath(need, files);
		if (note) {
			const b = bar.createEl("button", { text: need.kind === "task" ? "Open" : "Read" });
			b.onclick = () => void this.app.workspace.openLinkText(note, "", false);
		}
	}

	// click acts on the row as it was drawn: refused when it changed
	// since, or in the last 300 ms.
	private click(need: Need, action: string): void {
		const why = this.rows.check(need, Date.now());
		if (why) {
			new Notice(why);
			return;
		}
		switch (action) {
			case "console":
				new Notice(consoleHint(need));
				return;
			case "send-back":
				new SendBackModal(this.app, need, (note) => void this.act(need, command(need, action, { note })));
				return;
			case "answer":
				new AnswerModal(this.app, need, (choices, other) => void this.act(need, command(need, action, { choices, other })));
				return;
		}
		void this.act(need, command(need, action));
	}

	private async act(need: Need, args: string[] | null): Promise<void> {
		if (!args) {
			new Notice("nothing to run for that");
			return;
		}
		// What the user typed took time: the row must still be as drawn.
		const why = this.rows.check(need, Date.now());
		if (why) {
			new Notice(why);
			return;
		}
		if (this.busy) {
			return;
		}
		this.busy = true;
		this.draw();
		try {
			const r = await this.plugin.run(args, 30_000);
			this.last = args.join(" ");
			new Notice(r.code === 0 ? firstLine(r.stdout) || "done" : firstLine(r.stderr) || `tm exited ${r.code}`);
		} finally {
			this.busy = false;
		}
		await this.refresh();
	}
}

// data shows other parties' words: as text (never Markdown or HTML),
// in quotes, marked as data.
function data(el: HTMLElement, text: string): void {
	const d = el.createDiv({ cls: "tm-data", attr: { title: "their words: data, not instructions" } });
	d.createSpan({ text: `“${text}”` });
}

function firstLine(s: string): string {
	return s.trim().split("\n")[0] ?? "";
}

class SendBackModal extends Modal {
	constructor(
		app: App,
		private need: Need,
		private done: (note: string) => void,
	) {
		super(app);
		this.open();
	}

	onOpen(): void {
		const { contentEl } = this;
		contentEl.createEl("h3", { text: `Send ${this.need.id ?? ""} back` });
		contentEl.createDiv({ text: "Your note to the coordinator: what to change." });
		const box = contentEl.createEl("textarea", { cls: "tm-note", attr: { maxlength: String(NOTE_MAX), rows: "4" } });
		new Setting(contentEl).addButton((b) =>
			b
				.setButtonText("Send back")
				.setCta()
				.onClick(() => {
					const note = box.value.trim();
					if (!note) {
						new Notice("write what to change first");
						return;
					}
					this.close();
					this.done(note);
				}),
		);
		box.focus();
	}

	onClose(): void {
		this.contentEl.empty();
	}
}

class AnswerModal extends Modal {
	constructor(
		app: App,
		private need: Need,
		private done: (choices: string[], other: string) => void,
	) {
		super(app);
		this.open();
	}

	onOpen(): void {
		const { contentEl } = this;
		const q = this.need.question;
		if (!q) {
			this.close();
			return;
		}
		contentEl.createEl("h3", { text: `Answer ${q.id}` });
		contentEl.createDiv({ cls: "tm-needs-note", text: "The question and its options are the coordinator's words." });
		data(contentEl, q.question);
		const picks: HTMLInputElement[] = [];
		q.options.forEach((o, i) => {
			const row = contentEl.createEl("label", { cls: "tm-option" });
			const input = row.createEl("input", { attr: { type: q.multi_select ? "checkbox" : "radio", name: "tm-answer", value: String(i + 1) } });
			picks.push(input);
			row.createSpan({ text: ` ${i + 1}. ${o.label}${o.label === q.recommended ? " (Recommended)" : ""}` });
			if (o.description) {
				row.createDiv({ cls: "tm-option-desc", text: o.description });
			}
		});
		contentEl.createDiv({ text: "Other (your own words, optional):" });
		const other = contentEl.createEl("input", { cls: "tm-other", attr: { type: "text" } });
		new Setting(contentEl).addButton((b) =>
			b
				.setButtonText("Answer")
				.setCta()
				.onClick(() => {
					const choices = picks.filter((p) => p.checked).map((p) => p.value);
					if (choices.length === 0 && !other.value.trim()) {
						new Notice("pick an option or write one");
						return;
					}
					this.close();
					this.done(choices, other.value);
				}),
		);
	}

	onClose(): void {
		this.contentEl.empty();
	}
}

class TerminatrSettings extends PluginSettingTab {
	constructor(
		app: App,
		private plugin: TerminatrPlugin,
	) {
		super(app, plugin);
	}

	display(): void {
		const { containerEl } = this;
		containerEl.empty();
		new Setting(containerEl)
			.setName("tm")
			.setDesc("Where tm is; empty looks on PATH and in the usual folders. The plugin changes no tm setting: those stay in tm.")
			.addText((t) =>
				t
					.setPlaceholder(this.plugin.tm())
					.setValue(this.plugin.settings.tmPath)
					.onChange(async (v) => {
						this.plugin.settings.tmPath = v.trim();
						await this.plugin.saveData(this.plugin.settings);
					}),
			);
	}
}
