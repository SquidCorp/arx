/**
 * Context Ratio Extension
 *
 * Shows a persistent status line with the current context window utilization.
 * Updates after each turn and model change.
 */

import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";

function formatRatio(used: number, total: number): string {
	if (total <= 0) return "?";
	const pct = Math.round((used / total) * 100);
	return `${pct}%`;
}

function ratioColor(pct: number): string {
	if (pct < 50) return "success";
	if (pct < 80) return "warning";
	return "error";
}

function updateStatus(ctx: Parameters<Parameters<ExtensionAPI["on"]>[1]>[1]) {
	const usage = ctx.getContextUsage();
	const model = ctx.model;

	if (!usage || !model) {
		ctx.ui.setStatus("context-ratio", undefined);
		return;
	}

	const used = usage.tokens;
	const total = model.contextWindow ?? 0;
	const pct = total > 0 ? Math.round((used / total) * 100) : 0;
	const color = ratioColor(pct);

	const theme = ctx.ui.theme;
	const label = theme.fg("dim", "ctx ");
	const value = theme.fg(color, `${formatRatio(used, total)}`);
	const detail = theme.fg("dim", ` (${used.toLocaleString()}/${total.toLocaleString()})`);

	ctx.ui.setStatus("context-ratio", label + value + detail);
}

export default function (pi: ExtensionAPI) {
	pi.on("session_start", async (_event, ctx) => {
		updateStatus(ctx);
	});

	pi.on("turn_end", async (_event, ctx) => {
		updateStatus(ctx);
	});

	pi.on("model_select", async (_event, ctx) => {
		updateStatus(ctx);
	});

	pi.on("session_compact", async (_event, ctx) => {
		updateStatus(ctx);
	});
}
