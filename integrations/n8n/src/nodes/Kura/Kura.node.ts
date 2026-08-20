import type {
	IDataObject,
	IExecuteFunctions,
	IHttpRequestMethods,
	INodeExecutionData,
	INodeType,
	INodeTypeDescription,
	JsonObject,
} from 'n8n-workflow';
import { LoggerProxy as Logger, NodeApiError, NodeConnectionTypes, sleep } from 'n8n-workflow';

const CRED = 'kuraApi';
const PAGE_LIMIT = 1000;
const JOB_POLL_INTERVAL_MS = 500;
// A job that never reaches a terminal state would otherwise pin this execution
// forever, and a library-wide scan is now polled the same way a single-series
// one is. The cap is generous enough for a full metadata refresh and still
// bounded, so a stuck job fails loudly instead of stacking executions.
const JOB_POLL_TIMEOUT_MS = 2 * 60 * 60 * 1000;
const ACTIONABLE_STATUSES = new Set(['complete', 'incomplete']);

export class Kura implements INodeType {
	description: INodeTypeDescription = {
		displayName: 'Kura',
		name: 'kura',
		group: ['transform'],
		version: 2,
		icon: {
			light: 'file:../../assets/kura.svg',
			dark: 'file:../../assets/kura.dark.svg',
		},
		subtitle: '={{$parameter["operation"] + " (" + $parameter["resource"] + ")"}}',
		description:
			'Interact with the Kura suite: library series, release ingest, and the matcher queue',
		documentationUrl: 'https://github.com/wyvernzora/kura/tree/main/integrations/n8n',
		codex: {
			categories: ['Data & Storage'],
			resources: {
				primaryDocumentation: [
					{
						url: 'https://github.com/wyvernzora/kura/tree/main/integrations/n8n',
					},
				],
			},
			alias: ['Anime library', 'Plex library', 'Media library', 'Release indexer'],
		},
		defaults: { name: 'Kura' },
		inputs: [NodeConnectionTypes.Main],
		outputs:
			'={{$parameter["operation"] === "show" && $parameter["errorOnNotFound"] === false ? ["main", "main"] : ["main"]}}',
		outputNames: ['tracked', 'untracked'],
		usableAsTool: true,
		credentials: [{ name: CRED, required: true }],
		properties: [
			{
				displayName: 'Resource',
				name: 'resource',
				type: 'options',
				noDataExpression: true,
				options: [
					{ name: 'Queue', value: 'queue' },
					{ name: 'Release', value: 'release' },
					{ name: 'Series', value: 'series' },
				],
				default: 'series',
			},
			{
				displayName: 'Operation',
				name: 'operation',
				type: 'options',
				noDataExpression: true,
				displayOptions: { show: { resource: ['series'] } },
				options: [
					{ name: 'List', value: 'list', action: 'List actionable series' },
					{ name: 'Scan', value: 'scan', action: 'Scan a series' },
					{ name: 'Scan Library', value: 'scanAll', action: 'Scan the whole library' },
					{ name: 'Show', value: 'show', action: 'Show series state' },
					{ name: 'Update Tags', value: 'updateTags', action: 'Update tags on a series' },
				],
				default: 'list',
			},
			{
				displayName: 'Operation',
				name: 'operation',
				type: 'options',
				noDataExpression: true,
				displayOptions: { show: { resource: ['release'] } },
				options: [
					{ name: 'Get', value: 'get', action: 'Get a release' },
					{
						name: 'Get Magnet Link',
						value: 'getMagnetLink',
						action: 'Get a release magnet link',
					},
					{ name: 'Ingest', value: 'ingest', action: 'Ingest releases' },
					{ name: 'Set Status', value: 'setStatus', action: 'Set a release match status' },
				],
				default: 'ingest',
			},
			{
				displayName: 'Operation',
				name: 'operation',
				type: 'options',
				noDataExpression: true,
				displayOptions: { show: { resource: ['queue'] } },
				options: [
					{ name: 'Claim', value: 'claim', action: 'Claim a batch of releases' },
					{ name: 'Get Queue Stats', value: 'queueStats', action: 'Read the queue counts' },
					{ name: 'Submit Dispositions', value: 'submit', action: 'Submit disposition results' },
				],
				default: 'claim',
			},

			// ----- series (library-manager) -----
			{
				displayName: 'Statuses',
				name: 'statuses',
				type: 'multiOptions',
				options: [
					{ name: 'Complete', value: 'complete' },
					{ name: 'Incomplete', value: 'incomplete' },
				],
				default: ['incomplete'],
				description: 'Series statuses to include. Error and untracked rows are always skipped.',
				displayOptions: { show: { resource: ['series'], operation: ['list'] } },
			},
			{
				displayName: 'Simplify Output',
				name: 'simplifyOutput',
				type: 'boolean',
				default: true,
				description: 'Whether to return an agent-focused projection instead of the native Kura REST shape',
				displayOptions: { show: { resource: ['series'], operation: ['list', 'show'] } },
			},
			{
				displayName: 'Airing',
				name: 'airing',
				type: 'options',
				options: [
					{ name: 'Airing', value: 'airing' },
					{ name: 'Any', value: 'any' },
					{ name: 'Not Airing', value: 'notAiring' },
				],
				default: 'any',
				description: 'Filter by Kura observed-airing flag',
				displayOptions: { show: { resource: ['series'], operation: ['list'] } },
			},
			{
				displayName: 'Tags',
				name: 'tags',
				type: 'string',
				default: '',
				placeholder: 'priority:high !maintenance:disabled',
				description: 'Space-delimited tag filter. Plain tags must be present; !tag expressions must be absent.',
				displayOptions: { show: { resource: ['series'], operation: ['list'] } },
			},
			{
				displayName: 'Ref',
				name: 'ref',
				type: 'string',
				default: '={{$json.ref}}',
				required: true,
				description: 'Kura canonical ref, for example tvdb:370070',
				displayOptions: { show: { resource: ['series'], operation: ['scan', 'show', 'updateTags'] } },
			},
			{
				displayName: 'Metadata Only',
				name: 'metadataOnly',
				type: 'boolean',
				default: false,
				description:
					'Whether to refresh the provider spine, artwork, aliases, and search data without scanning files',
				displayOptions: { show: { resource: ['series'], operation: ['scan', 'scanAll'] } },
			},
			{
				displayName: 'Refresh Media',
				name: 'refresh',
				type: 'boolean',
				default: false,
				description:
					'Whether to re-probe active media even when file size and modification time are unchanged',
				displayOptions: { show: { resource: ['series'], operation: ['scan', 'scanAll'] } },
			},
			{
				displayName: 'Ordering',
				name: 'ordering',
				type: 'options',
				options: [
					{ name: 'Keep Existing', value: '' },
					{ name: 'Absolute', value: 'absolute' },
					{ name: 'Alternate', value: 'alternate' },
					{ name: 'Default', value: 'default' },
					{ name: 'DVD', value: 'dvd' },
					{ name: 'Official', value: 'official' },
					{ name: 'Regional', value: 'regional' },
				],
				default: '',
				description: 'Episode ordering to pin while refreshing the provider spine',
				displayOptions: { show: { resource: ['series'], operation: ['scan'] } },
			},
			{
				displayName: 'Concurrency',
				name: 'concurrency',
				type: 'number',
				typeOptions: { minValue: 0 },
				default: 0,
				description:
					'Series scanned in parallel. 0 uses the server default, which is tuned to stay inside provider rate limits.',
				displayOptions: { show: { resource: ['series'], operation: ['scanAll'] } },
			},
			{
				displayName: 'Tag Changes',
				name: 'tagChanges',
				type: 'string',
				default: '',
				required: true,
				placeholder: 'maintenance:requested !maintenance:disabled',
				description: 'Space-delimited changes. Plain tags add; !tag expressions remove.',
				displayOptions: { show: { resource: ['series'], operation: ['updateTags'] } },
			},
			{
				displayName: 'Episodes',
				name: 'episodes',
				type: 'string',
				default: '',
				placeholder: 'ALL, NONE, AIRING_SEASON, S1, or S1E1-12',
				description: 'Optional Kura episode selector. Empty means ALL.',
				displayOptions: { show: { resource: ['series'], operation: ['show'] } },
			},
			{
				displayName: 'Episode Status',
				name: 'status',
				type: 'multiOptions',
				options: [
					{ name: 'Pending', value: 'pending' },
					{ name: 'Missing', value: 'missing' },
					{ name: 'Present', value: 'present' },
					{ name: 'Staged', value: 'staged' },
					{ name: 'Staged Replacement', value: 'staged_replacement' },
				],
				default: [],
				description: 'Optional episode statuses to include',
				displayOptions: { show: { resource: ['series'], operation: ['show'] } },
			},
			{
				displayName: 'Include Specials',
				name: 'includeSpecials',
				type: 'boolean',
				default: false,
				description: 'Whether to include season 0 specials in the output',
				displayOptions: { show: { resource: ['series'], operation: ['show'] } },
			},
			{
				displayName: 'Source',
				name: 'source',
				type: 'string',
				default: '',
				placeholder: 'WebRip,BluRay',
				description: 'Optional comma-separated active-media sources',
				displayOptions: { show: { resource: ['series'], operation: ['show'] } },
			},
			{
				displayName: 'Resolution',
				name: 'resolution',
				type: 'string',
				default: '',
				placeholder: '1080p,4K',
				description: 'Optional comma-separated active-media resolutions',
				displayOptions: { show: { resource: ['series'], operation: ['show'] } },
			},
			{
				displayName: 'Error on Not Found',
				name: 'errorOnNotFound',
				type: 'boolean',
				default: true,
				description:
					'Whether to throw when Kura has no series for the ref. When off, an untracked ref is emitted on the second output instead; a ref resolving to several series still throws.',
				displayOptions: { show: { resource: ['series'], operation: ['show'] } },
			},

			// ----- release (release-indexer) -----
			{
				displayName: 'Posts',
				name: 'posts',
				type: 'json',
				default: '={{ $json.posts }}',
				required: true,
				description: 'The raw posts payload, forwarded as-is to /api/releases/v1/ingest',
				displayOptions: { show: { resource: ['release'], operation: ['ingest'] } },
			},
			{
				displayName: 'Infohash',
				name: 'infohash',
				type: 'string',
				default: '={{ $json.infohash }}',
				required: true,
				displayOptions: {
					show: { resource: ['release'], operation: ['get', 'getMagnetLink', 'setStatus'] },
				},
			},
			{
				displayName: 'Match Status',
				// NOT `status`: that key is already taken by the series Show
				// episode-status filter, and n8n's properties array is one flat
				// namespace (displayOptions gate visibility, not storage), so
				// sharing it would round-trip a multiOptions array into this
				// field and back.
				name: 'matchStatus',
				type: 'options',
				// Constrained to what the indexer's transition table accepts.
				// Every other match status belongs to the matcher's claim-fenced
				// submit path, which this operation deliberately cannot reach.
				options: [{ name: 'Dead', value: 'dead' }],
				default: 'dead',
				description: 'Match status to set. Only matched releases can be marked dead.',
				displayOptions: { show: { resource: ['release'], operation: ['setStatus'] } },
			},
			{
				displayName: 'Reason',
				name: 'reason',
				type: 'string',
				default: '',
				description: 'Optional note recorded to the release match event audit trail',
				displayOptions: { show: { resource: ['release'], operation: ['setStatus'] } },
			},

			// ----- queue (release-indexer) -----
			{
				displayName: 'Limit',
				name: 'limit',
				type: 'number',
				typeOptions: { minValue: 1 },
				default: 10,
				description: 'Max releases to claim',
				displayOptions: { show: { resource: ['queue'], operation: ['claim'] } },
			},
			{
				displayName: 'Lease Seconds',
				name: 'leaseSeconds',
				type: 'number',
				default: 300,
				description: 'Lease length; honored if supplied, else a server default',
				displayOptions: { show: { resource: ['queue'], operation: ['claim'] } },
			},
			{
				displayName: 'Body',
				name: 'body',
				type: 'json',
				default: '={{ $json }}',
				required: true,
				description:
					'A single queue submission, an array of submissions, an object with items, or a structured-output object with output.items',
				displayOptions: { show: { resource: ['queue'], operation: ['submit'] } },
			},
		],
	};

	async execute(this: IExecuteFunctions): Promise<INodeExecutionData[][]> {
		try {
			const resource = this.getNodeParameter('resource', 0) as string;
			if (resource === 'series') {
				return await executeSeries.call(this);
			}
			return await executeIndexer.call(this, resource);
		} catch (error) {
			if (this.continueOnFail()) {
				return [[{ json: { error: (error as Error).message }, pairedItem: { item: 0 } }]];
			}
			throw new NodeApiError(this.getNode(), error as JsonObject);
		}
	}
}

// ----- series resource: the library-manager REST API -----

async function executeSeries(this: IExecuteFunctions): Promise<INodeExecutionData[][]> {
	const operation = this.getNodeParameter('operation', 0) as string;
	const credentials = await this.getCredentials(CRED);
	const call = callFactory(this, credentials);

	if (operation === 'list') {
		const statuses = this.getNodeParameter('statuses', 0) as string[];
		const airing = this.getNodeParameter('airing', 0) as string;
		const simplifyOutput = this.getNodeParameter('simplifyOutput', 0) as boolean;
		const tags = this.getNodeParameter('tags', 0) as string;
		const rows = await listAll(call, statuses, airing, tags);
		return [
			rows
				.filter((row) => ACTIONABLE_STATUSES.has(stringField(row, 'status')))
				.filter((row) => stringField(row, 'ref') !== '')
				.map((row) => ({ json: projectRow(row, simplifyOutput) })),
		];
	}

	if (operation === 'updateTags') {
		const items = this.getInputData();
		const out: INodeExecutionData[] = [];
		for (let i = 0; i < items.length; i++) {
			const ref = this.getNodeParameter('ref', i) as string;
			const tags = splitTagExpressions(this.getNodeParameter('tagChanges', i) as string);
			try {
				if (tags.length === 0) throw new Error('at least one tag expression is required');
				const result = await call(
					'PATCH',
					`/api/library/v1/series/${encodeURIComponent(ref)}/tags`,
					{ tags },
				);
				out.push({ json: result, pairedItem: { item: i } });
			} catch (error) {
				if (this.continueOnFail()) {
					out.push({ json: { error: (error as Error).message }, pairedItem: { item: i } });
					continue;
				}
				throw new NodeApiError(this.getNode(), error as JsonObject, { itemIndex: i });
			}
		}
		return [out];
	}

	if (operation === 'scanAll') {
		// Library-wide, so it runs once per node execution rather than once
		// per item: fanning it out over N input items would submit N library
		// scans. The server does not reject those — same-kind submissions
		// join the running job, and a cross-kind collision (a concurrent
		// reindex) acks 202 with an empty job id — so the single submission
		// here is what keeps the behaviour well defined.
		const body: IDataObject = {
			metadataOnly: this.getNodeParameter('metadataOnly', 0) as boolean,
			refresh: this.getNodeParameter('refresh', 0) as boolean,
		};
		const concurrency = this.getNodeParameter('concurrency', 0) as number;
		if (concurrency > 0) body.concurrency = concurrency;
		try {
			const handle = await call('POST', '/api/library/v1/scan', body);
			const jobId = stringField(handle, 'jobId');
			const status = await waitForJob(call, jobId);
			// The tally sits nested under the terminal job status; surfacing
			// it flat would drop the job id an operator needs to correlate.
			Logger.info('Kura library scan completed', { job_id: jobId });
			return [[{ json: status }]];
		} catch (error) {
			if (this.continueOnFail()) {
				return [[{ json: { error: (error as Error).message } }]];
			}
			throw new NodeApiError(this.getNode(), error as JsonObject);
		}
	}

	if (operation === 'scan') {
		const items = this.getInputData();
		const out: INodeExecutionData[] = [];
		for (let i = 0; i < items.length; i++) {
			const ref = this.getNodeParameter('ref', i) as string;
			const body: IDataObject = {
				metadataOnly: this.getNodeParameter('metadataOnly', i) as boolean,
				refresh: this.getNodeParameter('refresh', i) as boolean,
			};
			const ordering = this.getNodeParameter('ordering', i) as string;
			if (ordering !== '') body.ordering = ordering;
			try {
				const handle = await call(
					'POST',
					`/api/library/v1/series/${encodeURIComponent(ref)}/scan`,
					body,
				);
				const result = await waitForJob(call, stringField(handle, 'jobId'));
				out.push({ json: result, pairedItem: { item: i } });
			} catch (error) {
				if (this.continueOnFail()) {
					out.push({ json: { error: (error as Error).message }, pairedItem: { item: i } });
					continue;
				}
				throw new NodeApiError(this.getNode(), error as JsonObject, { itemIndex: i });
			}
		}
		return [out];
	}

	const items = this.getInputData();
	const out: INodeExecutionData[] = [];
	const resolvedNotFound: INodeExecutionData[] = [];
	let emitUntrackedOutput = false;
	for (let i = 0; i < items.length; i++) {
		const errorOnNotFound = shouldErrorOnNotFound(this, i);
		if (!errorOnNotFound) emitUntrackedOutput = true;
		const ref = this.getNodeParameter('ref', i) as string;
		try {
			const includeSpecials = this.getNodeParameter('includeSpecials', i) as boolean;
			const simplifyOutput = this.getNodeParameter('simplifyOutput', i) as boolean;
			const result = await call('GET', showPath(ref, queryFor(this, i)));
			out.push({ json: projectShow(result, includeSpecials, simplifyOutput), pairedItem: { item: i } });
		} catch (error) {
			if (shouldResolveNotFound(errorOnNotFound, error)) {
				const result = await call('POST', '/api/library/v1/series/resolve', { terms: [ref] });
				resolvedNotFound.push({
					json: untrackedItem(result, ref),
					pairedItem: { item: i },
				});
				continue;
			}
			if (this.continueOnFail()) {
				out.push({ json: { error: (error as Error).message }, pairedItem: { item: i } });
				continue;
			}
			throw new NodeApiError(this.getNode(), error as JsonObject, { itemIndex: i });
		}
	}
	return emitUntrackedOutput ? [out, resolvedNotFound] : [out];
}

// ----- release + queue resources: the release-indexer REST API -----

async function executeIndexer(
	this: IExecuteFunctions,
	resource: string,
): Promise<INodeExecutionData[][]> {
	const items = this.getInputData();
	const operation = this.getNodeParameter('operation', 0) as string;

	const credentials = await this.getCredentials(CRED);
	const call = callFactory(this, credentials);
	Logger.info('Kura indexer execution started', { resource, operation, item_count: items.length });

	if (operation === 'queueStats') {
		const stats = await call('GET', '/api/releases/v1/queue/stats');
		Logger.info('Kura queue stats fetched', {
			available: stats.available,
			leased: stats.leased,
			exhausted: stats.exhausted,
		});
		return [[{ json: stats }]];
	}

	if (operation === 'claim') {
		const res = await call('POST', '/api/releases/v1/queue/claim', {
			limit: Number(this.getNodeParameter('limit', 0)),
			leaseSeconds: Number(this.getNodeParameter('leaseSeconds', 0)),
		});
		const claimed = (res.items as IDataObject[]) ?? [];
		Logger.info('Kura queue claim completed', { claimed_count: claimed.length });
		return [[{ json: { items: claimed, count: claimed.length } }]];
	}

	const out: INodeExecutionData[] = [];
	for (let i = 0; i < items.length; i++) {
		try {
			if (operation === 'ingest') {
				const posts = this.getNodeParameter('posts', i) as IDataObject[];
				const res = await call('POST', '/api/releases/v1/ingest', { posts });
				Logger.info('Kura ingest completed', {
					item_index: i,
					post_count: Array.isArray(posts) ? posts.length : undefined,
					new_count: (res.batch as IDataObject | undefined)?.new,
					updated_count: (res.batch as IDataObject | undefined)?.updated,
					duplicate_count: (res.batch as IDataObject | undefined)?.duplicate,
				});
				out.push({ json: res, pairedItem: { item: i } });
				continue;
			}

			if (operation === 'getMagnetLink') {
				const infohash = String(this.getNodeParameter('infohash', i));
				const res = await call('GET', `/api/releases/v1/${encodeURIComponent(infohash)}/magnet`);
				Logger.info('Kura magnet lookup completed', { item_index: i, infohash });
				out.push({ json: res, pairedItem: { item: i } });
				continue;
			}

			if (operation === 'setStatus') {
				const infohash = String(this.getNodeParameter('infohash', i));
				const status = String(this.getNodeParameter('matchStatus', i));
				const body: IDataObject = { status };
				const reason = String(this.getNodeParameter('reason', i));
				if (reason !== '') body.reason = reason;
				const res = await call(
					'PUT',
					`/api/releases/v1/${encodeURIComponent(infohash)}/status`,
					body,
				);
				Logger.info('Kura release status set', { item_index: i, infohash, status });
				out.push({ json: res, pairedItem: { item: i } });
				continue;
			}

			if (operation === 'get') {
				const infohash = String(this.getNodeParameter('infohash', i));
				const res = await call('GET', `/api/releases/v1/${encodeURIComponent(infohash)}`);
				Logger.info('Kura release lookup completed', { item_index: i, infohash });
				out.push({ json: res, pairedItem: { item: i } });
				continue;
			}

			const payload = submitPayload(this.getNodeParameter('body', i));
			const submitted: IDataObject[] = [];
			for (const body of payload.bodies) {
				submitted.push(await submitOne(this, call, body, i));
			}
			Logger.info('Kura submissions completed', {
				item_index: i,
				submit_count: submitted.length,
				conflict_count: submitted.filter((item) => item.ok === false).length,
			});
			out.push({
				json: { items: submitted, count: submitted.length },
				pairedItem: { item: i },
			});
		} catch (error) {
			const meta = {
				resource,
				operation,
				item_index: i,
				status_code: statusCode(error),
				err: (error as Error).message,
			};
			if (this.continueOnFail()) {
				Logger.warn('Kura indexer item failed', meta);
				out.push({ json: { error: (error as Error).message }, pairedItem: { item: i } });
				continue;
			}
			Logger.debug('Kura indexer item failed', meta);
			throw new NodeApiError(this.getNode(), error as JsonObject, { itemIndex: i });
		}
	}
	return [out];
}

function shouldErrorOnNotFound(ctx: IExecuteFunctions, itemIndex: number): boolean {
	return ctx.getNodeParameter('errorOnNotFound', itemIndex, true) !== false;
}

export function shouldResolveNotFound(errorOnNotFound: boolean, error: unknown): boolean {
	return !errorOnNotFound && isNotFoundError(error);
}

export function isNotFoundError(error: unknown): boolean {
	if (!isObject(error)) return false;
	return (
		error.httpCode === '404' ||
		error.status === 404 ||
		error.statusCode === 404 ||
		objectField(error, 'response')?.status === 404 ||
		objectField(error, 'response')?.statusCode === 404
	);
}

// A ref Kura does not track is exactly the case errorOnNotFound=false exists
// to make branchable, so zero candidates emits the ref rather than throwing.
// Ambiguity is a different condition: several candidates means the ref
// resolves to more than one series, and quietly picking one would be worse
// than failing.
export function untrackedItem(result: IDataObject, ref: string): IDataObject {
	const candidates = arrayField(result, 'candidates');
	if (candidates.length > 1) {
		throw new Error(`resolve returned ${candidates.length} candidates for ref ${ref}`);
	}
	return candidates.length === 0 ? { ref } : candidates[0];
}

type HTTPCall = (method: IHttpRequestMethods, path: string, body?: IDataObject) => Promise<IDataObject>;

async function waitForJob(call: HTTPCall, jobId: string): Promise<IDataObject> {
	if (jobId === '') throw new Error('Kura scan submission did not return a job ID');
	const path = `/api/library/v1/jobs/${encodeURIComponent(jobId)}`;
	const deadline = Date.now() + JOB_POLL_TIMEOUT_MS;
	for (;;) {
		const status = await call('GET', path);
		const state = stringField(status, 'state');
		if (state === 'succeeded') return status;
		if (state === 'failed') {
			const jobError = objectField(status, 'error');
			const kind = jobError ? stringField(jobError, 'kind') : '';
			const message = jobError ? stringField(jobError, 'message') : '';
			throw new Error(
				[kind, message].filter((part) => part !== '').join(': ') || 'Kura scan job failed',
			);
		}
		if (state !== 'queued' && state !== 'running') {
			throw new Error(`Kura scan job returned unknown state ${JSON.stringify(state)}`);
		}
		if (Date.now() >= deadline) {
			throw new Error(
				`Kura scan job ${jobId} did not finish within ${JOB_POLL_TIMEOUT_MS}ms (last state ${state})`,
			);
		}
		await sleep(JOB_POLL_INTERVAL_MS);
	}
}

function callFactory(ctx: IExecuteFunctions, credentials: IDataObject): HTTPCall {
	const baseUrl = String(credentials.baseUrl).replace(/\/+$/, '');
	return (method, path, body) =>
		ctx.helpers.httpRequestWithAuthentication.call(ctx, CRED, {
			method,
			url: `${baseUrl}${path}`,
			body: body as JsonObject | undefined,
			json: true,
		}) as Promise<IDataObject>;
}

async function listAll(call: HTTPCall, statuses: string[], airing: string, tags: string): Promise<IDataObject[]> {
	const rows: IDataObject[] = [];
	let cursor = '';
	do {
		const query = new URLSearchParams();
		const effectiveStatuses = statuses.length === 0 ? ['complete', 'incomplete'] : statuses;
		for (const status of effectiveStatuses) query.append('status', status);
		if (airing === 'airing') query.set('airing', 'true');
		if (airing === 'notAiring') query.set('airing', 'false');
		if (tags.trim() !== '') query.set('tags', tags);
		query.set('limit', String(PAGE_LIMIT));
		if (cursor !== '') query.set('cursor', cursor);

		const result = await call('GET', `/api/library/v1/series?${query.toString()}`);
		rows.push(...arrayField(result, 'items'));
		cursor = stringField(result, 'nextCursor');
	} while (cursor !== '');
	return rows;
}

export function projectRow(row: IDataObject, simplifyOutput = true): IDataObject {
	if (!simplifyOutput) return row;
	const out = dropEmpty({
		ref: row.ref,
		title: row.title,
		canonicalTitle: row.canonicalTitle,
		status: row.status,
		isAiring: Boolean(row.isAiring),
		staged: Boolean(row.staged),
		// Scan recency is kura-authored scheduling state; keeping it in the
		// simplified projection lets rotation/rescan Code nodes stay off the
		// heavy native rows. dropEmpty omits it for never-scanned series,
		// matching the wire's omitempty.
		lastScanned: row.lastScanned,
	});
	out.tags = stringArrayField(row, 'tags');
	return out;
}

function queryFor(ctx: IExecuteFunctions, itemIndex: number): URLSearchParams {
	const query = new URLSearchParams();
	const episodes = ctx.getNodeParameter('episodes', itemIndex) as string;
	if (episodes !== '') query.set('episodes', episodes);
	for (const status of ctx.getNodeParameter('status', itemIndex) as string[]) {
		query.append('status', status);
	}
	for (const source of csv(ctx.getNodeParameter('source', itemIndex) as string)) {
		query.append('source', source);
	}
	for (const resolution of csv(ctx.getNodeParameter('resolution', itemIndex) as string)) {
		query.append('resolution', resolution);
	}
	return query;
}

function showPath(ref: string, query: URLSearchParams): string {
	const suffix = query.toString();
	const path = `/api/library/v1/series/${encodeURIComponent(ref)}`;
	return suffix === '' ? path : `${path}?${suffix}`;
}

function csv(value: string): string[] {
	return value
		.split(',')
		.map((part) => part.trim())
		.filter((part) => part !== '');
}

export function splitTagExpressions(value: string): string[] {
	return value.trim() === '' ? [] : value.trim().split(/\s+/);
}

export function projectShow(
	show: IDataObject,
	includeSpecials = false,
	simplifyOutput = true,
): IDataObject {
	if (!simplifyOutput) return withSpecialsFilter(show, includeSpecials);

	const out = dropEmpty({
		ref: show.ref,
		preferredTitle: show.preferredTitle,
		canonicalTitle: show.canonicalTitle,
		status: show.status,
		isAiring: Boolean(show.isAiring),
	});
	out.tags = stringArrayField(show, 'tags');
	out.seasons = filteredSeasons(show, includeSpecials).map(projectSeason);
	return out;
}

function withSpecialsFilter(show: IDataObject, includeSpecials: boolean): IDataObject {
	const out: IDataObject = {
		...show,
		tags: stringArrayField(show, 'tags'),
	};
	if (!includeSpecials) out.seasons = filteredSeasons(show, false);
	return out;
}

function filteredSeasons(show: IDataObject, includeSpecials: boolean): IDataObject[] {
	return arrayField(show, 'seasons').filter((season) => includeSpecials || season.number !== 0);
}

function projectSeason(season: IDataObject): IDataObject {
	return dropEmpty({
		number: season.number,
		episodes: arrayField(season, 'episodes').map(projectEpisode),
	});
}

function projectEpisode(episode: IDataObject): IDataObject {
	return dropEmpty({
		episode: episode.episode,
		aired: episode.aired,
		status: episode.status,
		activeMediaFile: projectMediaFile(objectField(episode, 'active')),
		stagedMediaFile: projectMediaFile(objectField(episode, 'staged')),
	});
}

function projectMediaFile(media: IDataObject | undefined): IDataObject | undefined {
	if (!media) return undefined;
	return dropEmpty({
		resolution: media.resolution,
		source: media.source,
		codec: media.codec,
		attrs: media.attrs,
		hasSubtitles: hasSubtitles(media),
	});
}

function hasSubtitles(media: IDataObject): boolean {
	return arrayField(media, 'companions').some((companion) => {
		const role = stringField(companion, 'role').toLowerCase();
		const path = stringField(companion, 'path').toLowerCase();
		return (
			role === 'subtitle' ||
			path.endsWith('.ass') ||
			path.endsWith('.srt') ||
			path.endsWith('.ssa') ||
			path.endsWith('.vtt')
		);
	});
}

async function submitOne(
	ctx: IExecuteFunctions,
	call: HTTPCall,
	body: IDataObject,
	itemIndex: number,
): Promise<IDataObject> {
	try {
		await call('POST', '/api/releases/v1/queue/submit', body);
		return { infohash: submitInfohash(body), ref: submitRef(body), ok: true };
	} catch (error) {
		if (statusCode(error) === 409) {
			return { infohash: submitInfohash(body), ref: submitRef(body), ok: false, error: 'conflict' };
		}
		throw new NodeApiError(ctx.getNode(), error as JsonObject, { itemIndex });
	}
}

function submitPayload(input: unknown): { bodies: IDataObject[] } {
	if (typeof input === 'string') {
		return submitPayload(JSON.parse(input));
	}
	if (Array.isArray(input)) {
		return { bodies: input.map(asSubmitObject) };
	}
	const body = asSubmitObject(input);
	const items = body.items ?? outputItems(body);
	if (Array.isArray(items)) {
		return { bodies: items.map(asSubmitObject) };
	}
	return { bodies: [body] };
}

function asSubmitObject(input: unknown): IDataObject {
	if (input && typeof input === 'object' && !Array.isArray(input)) {
		return input as IDataObject;
	}
	throw new Error('Submit body must be an object, an array of objects, or an object with items');
}

function outputItems(body: IDataObject): unknown {
	const output = body.output;
	if (output && typeof output === 'object' && !Array.isArray(output)) {
		return (output as IDataObject).items;
	}
	return undefined;
}

function submitInfohash(body: IDataObject): string {
	const infohash = body.infohash;
	return typeof infohash === 'string' ? infohash : '';
}

function submitRef(body: IDataObject): string {
	const ref = body.ref;
	return typeof ref === 'string' ? ref : '';
}

function statusCode(error: unknown): number | undefined {
	if (!error || typeof error !== 'object') return undefined;
	const err = error as IDataObject;
	const response = err.response;
	const nested = response && typeof response === 'object' && !Array.isArray(response) ? (response as IDataObject) : {};
	for (const value of [err.statusCode, err.httpCode, nested.statusCode, nested.status]) {
		if (typeof value === 'number') return value;
		if (typeof value === 'string') {
			const parsed = Number.parseInt(value, 10);
			if (!Number.isNaN(parsed)) return parsed;
		}
	}
	return undefined;
}

function arrayField(obj: IDataObject, key: string): IDataObject[] {
	const value = obj[key];
	return Array.isArray(value) ? (value.filter(isObject) as IDataObject[]) : [];
}

function stringField(obj: IDataObject, key: string): string {
	const value = obj[key];
	return typeof value === 'string' ? value : '';
}

function stringArrayField(obj: IDataObject, key: string): string[] {
	const value = obj[key];
	return Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string') : [];
}

function objectField(obj: IDataObject, key: string): IDataObject | undefined {
	const value = obj[key];
	return isObject(value) ? value : undefined;
}

function isObject(value: unknown): value is IDataObject {
	return value !== null && typeof value === 'object' && !Array.isArray(value);
}

function dropEmpty(obj: IDataObject): IDataObject {
	const out: IDataObject = {};
	for (const [key, value] of Object.entries(obj)) {
		if (value === undefined || value === null || value === '') continue;
		if (Array.isArray(value) && value.length === 0) continue;
		out[key] = value as IDataObject[string];
	}
	return out;
}
