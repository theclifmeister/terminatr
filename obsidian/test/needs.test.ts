// The pane's logic without Obsidian: node --test test/ (node 22.18+
// runs TypeScript as is).
import assert from "node:assert/strict";
import { test } from "node:test";
import { type Need, Rows, SCHEMA, STABLE_MS, buttons, command, notePath, parseNeeds } from "../src/needs.ts";

const review: Need = { kind: "task", project: "demo", id: "T7", status: "review", summary: "T7 is in review", title: "Ship it", actions: ["accept", "send-back"] };
const question: Need = {
	kind: "question",
	project: "demo",
	id: "Q3",
	summary: "Q3 waits for your answer",
	question: { id: "Q3", header: "Repo", question: "Which repo?", options: [{ label: "Main" }, { label: "Site" }] },
	actions: ["answer"],
};

test("parseNeeds reads schema 1 and refuses others", () => {
	const ok = parseNeeds(JSON.stringify({ schema: SCHEMA, version: "1.2.3", running: true, needs: [review, { kind: 3 }] }));
	assert.ok(ok.ok);
	assert.equal(ok.list.needs.length, 1);
	assert.equal(ok.list.running, true);
	const old = parseNeeds(JSON.stringify({ schema: 0, needs: [] }));
	assert.ok(!old.ok && /update tm/.test(old.error));
	const newer = parseNeeds(JSON.stringify({ schema: SCHEMA + 1, needs: [] }));
	assert.ok(!newer.ok && /tm vault init --plugin/.test(newer.error));
	const junk = parseNeeds("usage: tm …");
	assert.ok(!junk.ok && /update tm/.test(junk.error));
});

test("command runs only the actions tm lists, as the human's commands", () => {
	assert.deepEqual(command(review, "accept"), ["task", "accept", "T7", "--project", "demo"]);
	assert.deepEqual(command(review, "send-back", { note: " -rf the tests " }), ["task", "send-back", "T7", "--note=-rf the tests", "--project", "demo"]);
	assert.equal(command(review, "send-back", { note: "  " }), null);
	assert.equal(command(review, "delegate"), null, "not an action tm listed");
	assert.equal(command({ ...review, actions: [] }, "accept"), null, "asked already");
	assert.equal(command({ ...review, id: "--approved-by-user" }, "accept"), null, "an id is never a flag");
	assert.equal(command({ ...review, project: "-x" }, "accept"), null);

	assert.deepEqual(command(question, "answer", { choices: ["2"] }), ["ask", "answer", "Q3", "--choice", "2", "--project", "demo"]);
	assert.deepEqual(command(question, "answer", { other: "both" }), ["ask", "answer", "Q3", "--other=both", "--project", "demo"]);
	assert.equal(command(question, "answer", { choices: ["3"] }), null, "no such option");
	assert.equal(command(question, "answer", { choices: ["1", "2"] }), null, "not multi-select");
	assert.equal(command(question, "answer", {}), null);

	const held: Need = { kind: "link-held", project: "demo", peer: "web", id: "M-18", summary: "", actions: ["approve", "drop"] };
	assert.deepEqual(command(held, "approve"), ["link", "approve", "M-18", "--project", "demo"]);
	assert.deepEqual(command(held, "drop"), ["link", "drop", "M-18", "--project", "demo"]);
	const paused: Need = { kind: "link-paused", project: "demo", peer: "web", id: "web", summary: "", actions: ["resume"] };
	assert.deepEqual(command(paused, "resume"), ["link", "resume", "web", "--project", "demo"]);
	const share: Need = { kind: "share-request", server: "mini", id: "S-1", summary: "", actions: ["decline", "console"] };
	assert.deepEqual(command(share, "decline"), ["peer", "decline", "S-1"]);
	assert.equal(command(share, "console"), null);
	// Accepting a link is the console's, like a file or a share; declining is the pane's too.
	const link: Need = { kind: "link-request", project: "demo", peer: "web", id: "R-4F2A-18", summary: "", actions: ["accept", "decline"] };
	assert.equal(command(link, "accept"), null);
	assert.deepEqual(command(link, "decline"), ["link", "decline", "R-4F2A-18", "--project", "demo"]);
});

test("buttons leave a link request's accept to the console", () => {
	assert.deepEqual(buttons({ kind: "link-request", project: "demo", id: "R-1", summary: "", actions: ["accept", "decline"] }), ["console", "decline"]);
	assert.deepEqual(buttons({ kind: "share-request", server: "mini", id: "S-1", summary: "", actions: ["decline", "console"] }), ["decline", "console"]);
	assert.deepEqual(buttons(review), ["accept", "send-back"]);
});

test("Rows: a click acts only on a row unchanged for 300 ms and as drawn", () => {
	const rows = new Rows();
	rows.update([review], 1000);
	assert.match(rows.check(review, 1000 + STABLE_MS - 1), /just changed/);
	assert.equal(rows.check(review, 1000 + STABLE_MS), "");
	// The same row on the next refresh keeps its time.
	rows.update([{ ...review }], 5000);
	assert.equal(rows.check(review, 5001), "");
	// Changed: new time, and the row as drawn before no longer matches.
	const asked = { ...review, asked: "accept", actions: [] };
	rows.update([asked], 6000);
	assert.match(rows.check(review, 9000), /changed since it was drawn/);
	assert.match(rows.check(asked, 6100), /just changed/);
	rows.update([], 7000);
	assert.match(rows.check(asked, 9000), /gone/);
});

test("notePath finds the task note and the message's data note", () => {
	const files = ["demo/tasks/T7.md", "demo/messages/web/M-18.md", "web/messages/demo/M-18.md"];
	assert.equal(notePath(review, files), "demo/tasks/T7.md");
	assert.equal(notePath({ kind: "link-held", project: "demo", id: "M-18", summary: "", actions: [] }, files), "demo/messages/web/M-18.md");
	assert.equal(notePath(question, files), null);
	assert.equal(notePath({ ...review, id: "T8" }, files), null);
});
