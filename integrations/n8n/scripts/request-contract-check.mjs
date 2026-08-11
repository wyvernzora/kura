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

console.log('request-contract-check ok');
