import assert from 'node:assert/strict';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const { KuraApi } = require('../dist/credentials/KuraApi.credentials.js');
const { Kura } = require('../dist/nodes/Kura/Kura.node.js');
const { KuraQueueTrigger } = require('../dist/nodes/KuraQueueTrigger/KuraQueueTrigger.node.js');

const BASE_URL = 'http://gateway.test';
const REF = 'tvdb:370070';
const HASH = '0123456789abcdef0123456789abcdef01234567';
const AUTH = { Authorization: 'Bearer product-token' };

function harness({ parameters, responses, items = [{ json: {} }] }) {
	const calls = [];
	const queued = [...responses];
	const respond = async (request) => {
		calls.push(request);
		assert.notEqual(queued.length, 0, `unexpected request ${request.method} ${request.url}`);
		const response = queued.shift();
		if (response instanceof Error) throw response;
		return response;
	};
	return {
		calls,
		context: {
			continueOnFail: () => false,
			getCredentials: async (name) => {
				assert.equal(name, 'kuraApi');
				return { baseUrl: `${BASE_URL}/`, bearerToken: 'product-token' };
			},
			getInputData: () => items,
			getNode: () => ({
				id: 'kura-test-node',
				name: 'Kura',
				type: 'n8n-nodes-kura.kura',
				typeVersion: 2,
				position: [0, 0],
				parameters: {},
			}),
			getNodeParameter: (name, itemIndex, fallback) => {
				const value = parameters[name];
				if (typeof value === 'function') return value(itemIndex);
				return value === undefined ? fallback : value;
			},
			helpers: {
				httpRequestWithAuthentication: async (credentialsType, request) => {
					assert.equal(credentialsType, 'kuraApi');
					return await respond({ ...request, headers: AUTH });
				},
			},
		},
		assertDone() {
			assert.equal(queued.length, 0, 'not all queued HTTP responses were consumed');
		},
	};
}

async function execute(parameters, responses, items) {
	const test = harness({ parameters, responses, items });
	const output = await Kura.prototype.execute.call(test.context);
	test.assertDone();
	return { calls: test.calls, output };
}

function request(method, path, body) {
	return {
		method,
		url: BASE_URL + path,
		headers: AUTH,
		body,
		json: true,
	};
}

{
	const credential = new KuraApi();
	assert.equal(credential.name, 'kuraApi');
	assert.equal(credential.displayName, 'Kura API');
	assert.equal(credential.properties[0].default, 'http://kura:8080');
	assert.equal(credential.authenticate.type, 'generic');
	assert.equal(credential.test.request.url, '/api/v1/health');
	assert.equal(new Kura().description.version, 2);
	assert.deepEqual(new Kura().description.credentials, [{ name: 'kuraApi', required: true }]);
	assert.equal(new KuraQueueTrigger().description.version, 3);
	assert.deepEqual(new KuraQueueTrigger().description.credentials, [
		{ name: 'kuraApi', required: true },
	]);
}

{
	const { calls, output } = await execute(
		{
			resource: 'series',
			operation: 'list',
			statuses: ['complete', 'incomplete'],
			airing: 'airing',
			tags: 'priority:high !maintenance:disabled',
			simplifyOutput: true,
		},
		[
			{
				items: [
					{ ref: REF, title: 'Bookworm', status: 'incomplete', tags: [] },
					{ title: 'Untracked', status: 'untracked' },
				],
				nextCursor: 'page-2',
			},
			{
				items: [{ ref: 'tvdb:404', title: 'Other', status: 'complete', tags: [] }],
			},
		],
	);
	assert.deepEqual(calls, [
		request(
			'GET',
			'/api/library/v1/series?status=complete&status=incomplete&airing=true&tags=priority%3Ahigh+%21maintenance%3Adisabled&limit=1000',
		),
		request(
			'GET',
			'/api/library/v1/series?status=complete&status=incomplete&airing=true&tags=priority%3Ahigh+%21maintenance%3Adisabled&limit=1000&cursor=page-2',
		),
	]);
	assert.deepEqual(
		output[0].map((item) => item.json),
		[
			{
				ref: REF,
				title: 'Bookworm',
				status: 'incomplete',
				isAiring: false,
				staged: false,
				tags: [],
			},
			{
				ref: 'tvdb:404',
				title: 'Other',
				status: 'complete',
				isAiring: false,
				staged: false,
				tags: [],
			},
		],
	);
}

{
	const { calls, output } = await execute(
		{
			resource: 'series',
			operation: 'show',
			ref: REF,
			episodes: 'ALL',
			status: ['missing'],
			source: 'WebRip',
			resolution: '1080p',
			includeSpecials: false,
			simplifyOutput: true,
			errorOnNotFound: true,
		},
		[{ ref: REF, preferredTitle: 'Bookworm', status: 'incomplete', seasons: [], tags: [] }],
	);
	assert.deepEqual(calls, [
		request(
			'GET',
			'/api/library/v1/series/tvdb%3A370070?episodes=ALL&status=missing&source=WebRip&resolution=1080p',
		),
	]);
	assert.deepEqual(output[0][0].json, {
		ref: REF,
		preferredTitle: 'Bookworm',
		status: 'incomplete',
		isAiring: false,
		tags: [],
		seasons: [],
	});
}

{
	const notFound = Object.assign(new Error('not found'), { statusCode: 404 });
	const { calls, output } = await execute(
		{
			resource: 'series',
			operation: 'show',
			ref: REF,
			episodes: '',
			status: [],
			source: '',
			resolution: '',
			includeSpecials: false,
			simplifyOutput: true,
			errorOnNotFound: false,
		},
		[notFound, { candidates: [{ ref: REF, preferredTitle: 'Bookworm' }] }],
	);
	assert.deepEqual(calls, [
		request('GET', '/api/library/v1/series/tvdb%3A370070'),
		request('POST', '/api/library/v1/series/resolve', { terms: [REF] }),
	]);
	assert.deepEqual(output, [[], [{ json: { ref: REF, preferredTitle: 'Bookworm' }, pairedItem: { item: 0 } }]]);
}

// A ref Kura has never heard of is the whole point of errorOnNotFound=false:
// it must reach the untracked output, not abort the run.
{
	const notFound = Object.assign(new Error('not found'), { statusCode: 404 });
	const { calls, output } = await execute(
		{
			resource: 'series',
			operation: 'show',
			ref: REF,
			episodes: '',
			status: [],
			source: '',
			resolution: '',
			includeSpecials: false,
			simplifyOutput: true,
			errorOnNotFound: false,
		},
		[notFound, { candidates: [] }],
	);
	assert.deepEqual(calls, [
		request('GET', '/api/library/v1/series/tvdb%3A370070'),
		request('POST', '/api/library/v1/series/resolve', { terms: [REF] }),
	]);
	assert.deepEqual(output, [[], [{ json: { ref: REF }, pairedItem: { item: 0 } }]]);
}

{
	const { calls, output } = await execute(
		{
			resource: 'series',
			operation: 'updateTags',
			ref: REF,
			tagChanges: 'priority:high !maintenance:disabled',
		},
		[{ ref: REF, tags: ['priority:high'] }],
	);
	assert.deepEqual(calls, [
		request('PATCH', '/api/library/v1/series/tvdb%3A370070/tags', {
			tags: ['priority:high', '!maintenance:disabled'],
		}),
	]);
	assert.deepEqual(output[0][0], {
		json: { ref: REF, tags: ['priority:high'] },
		pairedItem: { item: 0 },
	});
}

{
	const secondRef = 'tvdb:404';
	const { calls, output } = await execute(
		{
			resource: 'series',
			operation: 'scan',
			ref: (itemIndex) => (itemIndex === 0 ? REF : secondRef),
			metadataOnly: (itemIndex) => itemIndex === 0,
			refresh: (itemIndex) => itemIndex === 1,
			ordering: (itemIndex) => (itemIndex === 0 ? '' : 'dvd'),
		},
		[
			{ jobId: 'scan-1', kind: 'scan', statusUrl: '/api/library/v1/jobs/scan-1' },
			{ jobId: 'scan-1', kind: 'scan', state: 'running' },
			{
				jobId: 'scan-1',
				kind: 'scan',
				ref: REF,
				state: 'succeeded',
				result: { synced: [], skipped: [], orphanSlots: [] },
			},
			{ jobId: 'scan-2', kind: 'scan', statusUrl: '/api/library/v1/jobs/scan-2' },
			{
				jobId: 'scan-2',
				kind: 'scan',
				ref: secondRef,
				state: 'succeeded',
				result: { synced: [], skipped: [], orphanSlots: [] },
			},
		],
		[{ json: { ref: REF } }, { json: { ref: secondRef } }],
	);
	assert.deepEqual(calls, [
		request('POST', '/api/library/v1/series/tvdb%3A370070/scan', {
			metadataOnly: true,
			refresh: false,
		}),
		request('GET', '/api/library/v1/jobs/scan-1'),
		request('GET', '/api/library/v1/jobs/scan-1'),
		request('POST', '/api/library/v1/series/tvdb%3A404/scan', {
			metadataOnly: false,
			refresh: true,
			ordering: 'dvd',
		}),
		request('GET', '/api/library/v1/jobs/scan-2'),
	]);
	assert.deepEqual(output, [
		[
			{
				json: {
					jobId: 'scan-1',
					kind: 'scan',
					ref: REF,
					state: 'succeeded',
					result: { synced: [], skipped: [], orphanSlots: [] },
				},
				pairedItem: { item: 0 },
			},
			{
				json: {
					jobId: 'scan-2',
					kind: 'scan',
					ref: secondRef,
					state: 'succeeded',
					result: { synced: [], skipped: [], orphanSlots: [] },
				},
				pairedItem: { item: 1 },
			},
		],
	]);
}

{
	let thrown;
	try {
		await execute(
			{
				resource: 'series',
				operation: 'scan',
				ref: REF,
				metadataOnly: true,
				refresh: false,
				ordering: '',
			},
			[
				{ jobId: 'scan-failed', kind: 'scan' },
				{
					jobId: 'scan-failed',
					kind: 'scan',
					state: 'failed',
					error: { kind: 'provider', message: 'TVDB unavailable' },
				},
			],
		);
	} catch (error) {
		thrown = error;
	}
	assert.equal(thrown?.constructor.name, 'NodeApiError');
	assert.match(thrown?.message ?? '', /provider: TVDB unavailable/);
}

{
	const posts = [
		{
			title: 'Bookworm 01',
			magnet: `magnet:?xt=urn:btih:${HASH}`,
			source: 'fixture',
			sourceId: 'fixture-1',
			url: 'https://example.invalid/fixture-1',
			publishedAt: '2026-07-28T00:00:00Z',
			sizeBytes: 123,
		},
	];
	const { calls, output } = await execute(
		{ resource: 'release', operation: 'ingest', posts },
		[{ batch: { new: 1, updated: 0, duplicate: 0 }, queue: { available: 1 } }],
	);
	assert.deepEqual(calls, [
		request('POST', '/api/releases/v1/ingest', { posts }),
	]);
	assert.equal(output[0][0].json.batch.new, 1);
}

{
	const { calls, output } = await execute(
		{ resource: 'release', operation: 'get', infohash: HASH },
		[{ infohash: HASH, matchStatus: 'matched', ref: REF }],
	);
	assert.deepEqual(calls, [request('GET', `/api/releases/v1/${HASH}`)]);
	assert.equal(output[0][0].json.ref, REF);
}

{
	const { calls, output } = await execute(
		{ resource: 'release', operation: 'getMagnetLink', infohash: HASH },
		[{ infohash: HASH, magnet: `magnet:?xt=urn:btih:${HASH}` }],
	);
	assert.deepEqual(calls, [request('GET', `/api/releases/v1/${HASH}/magnet`)]);
	assert.equal(output[0][0].json.infohash, HASH);
}

{
	const stats = {
		available: 1,
		leased: 2,
		unmatched: 3,
		matched: 4,
		suppressed: 5,
		exhausted: 6,
	};
	const { calls, output } = await execute(
		{ resource: 'queue', operation: 'queueStats' },
		[stats],
	);
	assert.deepEqual(calls, [request('GET', '/api/releases/v1/queue/stats')]);
	assert.deepEqual(output, [[{ json: stats }]]);
}

{
	const claim = { items: [{ infohash: HASH, claimToken: 42, rawItems: [] }] };
	const { calls, output } = await execute(
		{ resource: 'queue', operation: 'claim', limit: 4, leaseSeconds: 90 },
		[claim],
	);
	assert.deepEqual(calls, [
		request('POST', '/api/releases/v1/queue/claim', { limit: 4, leaseSeconds: 90 }),
	]);
	assert.deepEqual(output[0][0].json, { items: claim.items, count: 1 });
}

{
	const accepted = { infohash: HASH, claimToken: 42, status: 'matched', ref: REF };
	const stale = { infohash: 'abcdef0123456789abcdef0123456789abcdef01', claimToken: 43, status: 'unmatched' };
	const conflict = Object.assign(new Error('stale lease'), { response: { status: 409 } });
	const { calls, output } = await execute(
		{
			resource: 'queue',
			operation: 'submit',
			body: { output: { items: [accepted, stale] } },
		},
		[{ ok: true }, conflict],
	);
	assert.deepEqual(calls, [
		request('POST', '/api/releases/v1/queue/submit', accepted),
		request('POST', '/api/releases/v1/queue/submit', stale),
	]);
	assert.deepEqual(output[0][0].json, {
		items: [
			{ infohash: HASH, ref: REF, ok: true },
			{ infohash: stale.infohash, ref: '', ok: false, error: 'conflict' },
		],
		count: 2,
	});
}

{
	class AxiosError extends Error {}

	const httpMessage = {};
	const res = { socket: { _httpMessage: httpMessage } };
	httpMessage.res = res;

	const badRequest = new AxiosError('Request failed with status code 400');
	badRequest.response = {
		status: 400,
		data: { message: 'json: unknown field "claim_token"' },
	};
	badRequest.request = httpMessage;

	let thrown;
	try {
		await execute(
			{
				resource: 'queue',
				operation: 'submit',
				body: { infohash: HASH, claim_token: 42, status: 'matched', ref: REF },
			},
			[badRequest],
		);
	} catch (error) {
		thrown = error;
	}

	assert.equal(thrown?.constructor.name, 'NodeApiError');
	assert.equal(thrown?.httpCode, '400');
	assert.doesNotThrow(() =>
		JSON.stringify({
			...thrown,
			name: thrown.name,
			message: thrown.message,
			stack: thrown.stack,
		}),
	);
}

{
	const test = harness({
		parameters: { limit: 3, leaseSeconds: 120 },
		responses: [{ items: [{ infohash: HASH, claimToken: 42, rawItems: [] }] }],
	});
	const output = await KuraQueueTrigger.prototype.poll.call(test.context);
	test.assertDone();
	assert.deepEqual(test.calls, [
		request('POST', '/api/releases/v1/queue/claim', { limit: 3, leaseSeconds: 120 }),
	]);
	assert.deepEqual(output, [[{ json: { infohash: HASH, claimToken: 42, rawItems: [] } }]]);
}

// Each claim is its own n8n item, carrying the identity a submission has to
// fence on — so a workflow binds claimToken from the paired item instead of
// asking a model to restate it.
{
	const second = 'abcdef0123456789abcdef0123456789abcdef01';
	const test = harness({
		parameters: { limit: 3, leaseSeconds: 120 },
		responses: [
			{
				items: [
					{ infohash: HASH, claimToken: 42, rawItems: [] },
					{ infohash: second, claimToken: 43, rawItems: [] },
				],
			},
		],
	});
	const output = await KuraQueueTrigger.prototype.poll.call(test.context);
	test.assertDone();
	assert.deepEqual(output, [
		[
			{ json: { infohash: HASH, claimToken: 42, rawItems: [] } },
			{ json: { infohash: second, claimToken: 43, rawItems: [] } },
		],
	]);
}

{
	const test = harness({
		parameters: { limit: 3, leaseSeconds: 120 },
		responses: [{ items: [] }],
	});
	const output = await KuraQueueTrigger.prototype.poll.call(test.context);
	test.assertDone();
	assert.equal(output, null);
}

// --- series: scanAll ---
// Library-wide, so two input items must still produce exactly ONE scan
// submission: fanning out would launch competing library scans.
{
	const { calls, output } = await execute(
		{
			resource: 'series',
			operation: 'scanAll',
			metadataOnly: true,
			refresh: false,
			concurrency: 0,
		},
		[
			{ jobId: 'scanall-1', kind: 'scanAll', statusUrl: '/api/library/v1/jobs/scanall-1' },
			{ jobId: 'scanall-1', kind: 'scanAll', state: 'running' },
			{
				jobId: 'scanall-1',
				kind: 'scanAll',
				state: 'succeeded',
				result: { scanned: 412, failed: 0, series: [] },
			},
		],
		[{ json: {} }, { json: {} }],
	);
	assert.deepEqual(calls, [
		// concurrency 0 is omitted so the server default applies.
		request('POST', '/api/library/v1/scan', { metadataOnly: true, refresh: false }),
		request('GET', '/api/library/v1/jobs/scanall-1'),
		request('GET', '/api/library/v1/jobs/scanall-1'),
	]);
	// The terminal tally is surfaced nested under the job status, keeping the
	// job id available for correlation.
	assert.deepEqual(output, [
		[
			{
				json: {
					jobId: 'scanall-1',
					kind: 'scanAll',
					state: 'succeeded',
					result: { scanned: 412, failed: 0, series: [] },
				},
			},
		],
	]);
}

// An explicit concurrency rides through; 0 means "server default" and must not.
{
	const { calls } = await execute(
		{
			resource: 'series',
			operation: 'scanAll',
			metadataOnly: false,
			refresh: true,
			concurrency: 8,
		},
		[
			{ jobId: 'scanall-2', kind: 'scanAll' },
			{ jobId: 'scanall-2', kind: 'scanAll', state: 'succeeded', result: {} },
		],
		[{ json: {} }],
	);
	assert.deepEqual(calls, [
		request('POST', '/api/library/v1/scan', {
			metadataOnly: false,
			refresh: true,
			concurrency: 8,
		}),
		request('GET', '/api/library/v1/jobs/scanall-2'),
	]);
}

// --- release: setStatus ---
{
	const second = 'abcdefabcdefabcdefabcdefabcdefabcdefabcd';
	const { calls, output } = await execute(
		{
			resource: 'release',
			operation: 'setStatus',
			infohash: (itemIndex) => (itemIndex === 0 ? HASH : second),
			matchStatus: 'dead',
			// An empty reason is omitted rather than sent as "".
			reason: (itemIndex) => (itemIndex === 0 ? 'stalled at 0%' : ''),
		},
		[{ ok: true }, { ok: true }],
		[{ json: {} }, { json: {} }],
	);
	assert.deepEqual(calls, [
		request('PUT', `/api/releases/v1/${HASH}/status`, {
			status: 'dead',
			reason: 'stalled at 0%',
		}),
		request('PUT', `/api/releases/v1/${second}/status`, { status: 'dead' }),
	]);
	assert.deepEqual(output, [
		[
			{ json: { ok: true }, pairedItem: { item: 0 } },
			{ json: { ok: true }, pairedItem: { item: 1 } },
		],
	]);
}

// --- node description invariant: parameter names are one flat namespace ---
// n8n stores every property by `name` regardless of displayOptions, so two
// properties sharing a name share a storage slot: a value set under one
// operation round-trips into the other, across incompatible types. This is
// invisible to the per-operation scenarios above, which read parameters from
// a flat map keyed by name and therefore cannot reproduce the collision.
// Two properties sharing a `name` share one storage slot: n8n keys saved
// values by name alone, and displayOptions gate visibility only. A value set
// under one operation therefore round-trips into the other — across types
// (a multiOptions array arriving where a string is read) or within one (a
// stale enum value from a disjoint option set). Both are bugs, so the rule
// is a flat no-duplicates.
//
// `operation` is the one legitimate exception: declaring it once per resource
// is the n8n idiom, and n8n resets it when the resource changes.
const SHARED_PARAM_NAMES = new Set(['operation']);
{
	const byName = new Map();
	for (const prop of new Kura().description.properties) {
		if (SHARED_PARAM_NAMES.has(prop.name)) continue;
		if (!byName.has(prop.name)) byName.set(prop.name, []);
		byName.get(prop.name).push(prop);
	}
	for (const [name, props] of byName) {
		if (props.length > 1) {
			assert.fail(
				`node parameter ${JSON.stringify(name)} is declared ${props.length} times — by ` +
					`${props.map((p) => `${JSON.stringify(p.displayName)} (${p.type})`).join(' and ')} ` +
					`— and they share one storage slot`,
			);
		}
	}
}

console.log('request-contract-check ok');
