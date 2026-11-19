#!/usr/bin/env node
// Copies a published GitHub release and its assets to CNB Releases
// (cnb.cool/dbxio.com/motty), the download host used from inside mainland
// China. GitHub stays the source of record. The CNB release is created when the
// mirror does not have it and its metadata follows the GitHub one; an asset
// already on the mirror is skipped, so running again finishes a partial copy
// without uploading everything twice.
//
// Usage:
//   node tools/sync-cnb-release.mjs --github-release release.json --assets-dir DIR
//
//   release.json  what `gh release view TAG --json tagName,name,body,
//                 targetCommitish,isPrerelease` prints
//   DIR           the files `gh release download TAG --dir DIR` wrote
//
// Needs CNB_TOKEN, an access token with write access to dbxio.com/motty.
// CNB_API_BASE overrides the API host, which is what a local stub serves.

import { createReadStream, readdirSync, readFileSync, statSync } from "node:fs";
import { basename, join } from "node:path";
import { pathToFileURL } from "node:url";

const DEFAULT_API_BASE = "https://api.cnb.cool";
const REPOSITORY = "dbxio.com/motty";
const DEFAULT_CONCURRENCY = 3;
const MAX_ATTEMPTS = 4;

async function main() {
	const args = parseArgs(process.argv.slice(2));
	const githubRelease = JSON.parse(readFileSync(args.githubRelease, "utf8"));
	const tag = githubRelease.tagName;
	if (!tag) throw new Error("the GitHub release JSON names no tagName");

	const client = new CnbClient({
		apiBase: process.env.CNB_API_BASE || DEFAULT_API_BASE,
		repository: REPOSITORY,
		token: args.token,
	});
	const release = await client.ensureRelease(tag, githubRelease, { makeLatest: args.makeLatest });
	const mirrored = new Set((release.assets || []).map((asset) => asset.name));
	const local = localAssets(args.assetsDir);
	const missing = local.filter((path) => !mirrored.has(basename(path)));
	console.log(`CNB release ${tag}: ${mirrored.size} asset(s) already there, ${missing.length} to upload.`);
	await mapWithConcurrency(missing, args.concurrency, (path) =>
		uploadWithRetry(client, release.id, path, args.overwrite),
	);
}

function parseArgs(argv) {
	const args = {
		token: process.env.CNB_TOKEN || "",
		concurrency: Number.parseInt(process.env.CNB_UPLOAD_CONCURRENCY || `${DEFAULT_CONCURRENCY}`, 10),
		githubRelease: "",
		assetsDir: "",
		overwrite: false,
		makeLatest: false,
	};
	for (let index = 0; index < argv.length; index++) {
		const arg = argv[index];
		if (arg === "--github-release") args.githubRelease = argv[++index];
		else if (arg === "--assets-dir") args.assetsDir = argv[++index];
		else if (arg === "--overwrite-existing") args.overwrite = true;
		else if (arg === "--make-latest") args.makeLatest = true;
		else throw new Error(`unknown argument: ${arg}`);
	}
	if (!args.token) throw new Error("CNB_TOKEN is required");
	if (!args.githubRelease || !args.assetsDir) {
		throw new Error("usage: sync-cnb-release.mjs --github-release release.json --assets-dir DIR [--overwrite-existing] [--make-latest]");
	}
	if (!Number.isInteger(args.concurrency) || args.concurrency < 1) {
		throw new Error("CNB_UPLOAD_CONCURRENCY must be a positive integer");
	}
	return args;
}

export class CnbClient {
	constructor({ apiBase, repository, token }) {
		this.apiBase = apiBase.replace(/\/+$/, "");
		this.repository = repository;
		this.token = token;
	}

	// ensureRelease creates the CNB release when it is missing and keeps its
	// name, notes and prerelease flag equal to the GitHub one. The asset list of
	// an existing release is what says which uploads can be skipped.
	async ensureRelease(tag, githubRelease, { makeLatest = false } = {}) {
		const metadata = {
			name: githubRelease.name || tag,
			body: githubRelease.body || "",
			prerelease: Boolean(githubRelease.isPrerelease),
			...(makeLatest ? { make_latest: "true" } : {}),
		};
		const existing = await this.request(
			"GET",
			`/${this.repository}/-/releases/tags/${encodeURIComponent(tag)}`,
			null,
			true,
		);
		if (!existing) {
			return this.request("POST", `/${this.repository}/-/releases`, {
				tag_name: tag,
				target_commitish: githubRelease.targetCommitish || "",
				...metadata,
			});
		}
		await this.request("PATCH", `/${this.repository}/-/releases/${existing.id}`, metadata);
		return existing;
	}

	async uploadAsset(releaseId, filePath, overwrite) {
		const size = statSync(filePath).size;
		const target = await this.request("POST", `/${this.repository}/-/releases/${releaseId}/asset-upload-url`, {
			asset_name: basename(filePath),
			size,
			overwrite,
		});
		const upload = await fetch(target.upload_url, {
			method: "PUT",
			headers: { "Content-Type": "application/octet-stream", "Content-Length": `${size}` },
			body: createReadStream(filePath),
			duplex: "half",
		});
		if (!upload.ok) {
			throw new Error(`CNB object upload failed with ${upload.status}: ${await upload.text()}`);
		}
		const verify = await fetch(decodeURIComponent(target.verify_url), {
			method: "POST",
			headers: this.headers(),
		});
		if (!verify.ok) {
			throw new Error(`CNB upload confirmation failed with ${verify.status}: ${await verify.text()}`);
		}
	}

	async request(method, path, body = null, allow404 = false) {
		const response = await fetch(`${this.apiBase}${path}`, {
			method,
			headers: this.headers(body !== null),
			body: body === null ? undefined : JSON.stringify(body),
		});
		if (allow404 && response.status === 404) return null;
		if (!response.ok) {
			throw new Error(`CNB API ${method} ${path} failed with ${response.status}: ${await response.text()}`);
		}
		const text = await response.text();
		// CNB acknowledges metadata updates with 200 and an empty body.
		if (!text.trim()) return null;
		return JSON.parse(text);
	}

	headers(json = false) {
		return {
			Accept: "application/vnd.cnb.api+json",
			Authorization: `Bearer ${this.token}`,
			...(json ? { "Content-Type": "application/json" } : {}),
		};
	}
}

async function uploadWithRetry(client, releaseId, filePath, overwrite) {
	const name = basename(filePath);
	for (let attempt = 1; attempt <= MAX_ATTEMPTS; attempt++) {
		try {
			console.log(`${name}: uploading (attempt ${attempt}/${MAX_ATTEMPTS})`);
			await client.uploadAsset(releaseId, filePath, overwrite);
			console.log(`${name}: uploaded`);
			return;
		} catch (error) {
			if (attempt === MAX_ATTEMPTS) throw error;
			const delay = 2 ** (attempt - 1) * 5000;
			console.warn(`${name}: ${error.message}; retrying in ${delay / 1000}s`);
			await new Promise((resolve) => setTimeout(resolve, delay));
		}
	}
}

// localAssets sorts the largest file first, so the uploads that take longest
// start before the small signature and checksum files.
function localAssets(dir) {
	return readdirSync(dir)
		.map((name) => join(dir, name))
		.filter((path) => statSync(path).isFile())
		.sort((left, right) => statSync(right).size - statSync(left).size || basename(left).localeCompare(basename(right)));
}

async function mapWithConcurrency(items, concurrency, worker) {
	let next = 0;
	async function run() {
		while (next < items.length) {
			const index = next++;
			await worker(items[index]);
		}
	}
	await Promise.all(Array.from({ length: Math.min(concurrency, items.length) }, run));
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
	main().catch((error) => {
		console.error(error.message || error);
		process.exitCode = 1;
	});
}
